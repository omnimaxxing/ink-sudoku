package hwr

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"sync"
)

// model.bin format, version 1 (all little-endian). Written by
// hwrtrain/src/hwrtrain/modelbin.py, which can also read it back.
//
//	magic     [4]byte "HWR1"
//	version   u16 = 1
//	nclass    u16
//	labels    [nclass]byte   one ASCII label per output: '0'..'9' digit,
//	                         '?' reject (non-digit)
//	threshold f32            calibrated accept threshold (see Classify)
//	inC inH inW  u16 x3      input tensor shape (1, 28, 28)
//	nlayer    u16
//	layers    nlayer records, each starting with an op byte:
//	  op 1 CONV2D:  outC u16, inC u16, k u8, stride u8, pad u8, relu u8,
//	                scale [outC]f32, bias [outC]f32, w [outC*inC*k*k]i8 (OIHW)
//	  op 2 MAXPOOL: k u8, stride u8               (floor mode, no padding)
//	  op 3 DENSE:   out u16, in u16, relu u8,
//	                scale [out]f32, bias [out]f32, w [out*in]i8 (row = output)
//	crc32     u32            IEEE CRC of every preceding byte
//
// Weights are symmetric per-output-channel int8: w = q * scale[o]. BatchNorm
// is folded into conv weights/bias before quantisation. DENSE flattens its
// input in CHW order. Activations stay float32: the A53 has no int8 dot
// product instruction (ARMv8.0), and pure-Go int8 math is not faster than
// scalar float FMA. So weights are dequantised once at load.
//
// The PyTorch reference used for parity runs exactly these dequantised
// weights, see hwrtrain modelbin.py.

//go:embed model.bin
var embeddedModel []byte

const (
	opConv    = 1
	opMaxPool = 2
	opDense   = 3
)

type layer struct {
	op                   int
	outC, inC, k, stride int
	pad                  int
	relu                 bool
	w, bias              []float32
	inShape, outShape    [3]int // C,H,W
	colLen               int    // im2col buffer length for conv
}

// Model is a loaded classifier. It is safe for concurrent use.
type Model struct {
	// Labels holds one byte per output ('0'..'9' or '?').
	Labels []byte
	// Threshold is the calibrated accept threshold stored in the file.
	Threshold float32
	// ID is the file's CRC32, handy for logging which model made a guess.
	ID uint32

	in     [3]int
	layers []layer
	maxAct int
	maxCol int
	// embedAt is the first dense layer; Embed returns its input.
	embedAt int
	pool    sync.Pool
}

var (
	defaultOnce  sync.Once
	defaultModel *Model
)

// DefaultModel returns the model embedded in the binary (backend/hwr/
// model.bin). It panics if that file is corrupt, which tests catch.
func DefaultModel() *Model {
	defaultOnce.Do(func() {
		m, err := ParseModel(embeddedModel)
		if err != nil {
			panic("hwr: embedded model.bin: " + err.Error())
		}
		defaultModel = m
	})
	return defaultModel
}

// LoadModel reads a model.bin stream.
func LoadModel(r io.Reader) (*Model, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseModel(b)
}

type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.off+n > len(r.b) {
		r.err = io.ErrUnexpectedEOF
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}
func (r *reader) u8() int {
	s := r.take(1)
	if s == nil {
		return 0
	}
	return int(s[0])
}
func (r *reader) u16() int {
	s := r.take(2)
	if s == nil {
		return 0
	}
	return int(binary.LittleEndian.Uint16(s))
}
func (r *reader) f32s(n int) []float32 {
	s := r.take(4 * n)
	if s == nil {
		return nil
	}
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(s[4*i:]))
	}
	return out
}

// dequant reads n int8 weights laid out as rows of rowLen and multiplies
// row o by scale[o].
func (r *reader) dequant(scale []float32, rowLen int) []float32 {
	s := r.take(len(scale) * rowLen)
	if s == nil {
		return nil
	}
	out := make([]float32, len(s))
	for i, q := range s {
		out[i] = float32(int8(q)) * scale[i/rowLen]
	}
	return out
}

// ParseModel decodes a model.bin image.
func ParseModel(b []byte) (*Model, error) {
	if len(b) < 8 || !bytes.Equal(b[:4], []byte("HWR1")) {
		return nil, errors.New("hwr: not a HWR1 model")
	}
	body, sum := b[:len(b)-4], binary.LittleEndian.Uint32(b[len(b)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return nil, errors.New("hwr: model checksum mismatch")
	}
	r := &reader{b: body, off: 4}
	if v := r.u16(); v != 1 {
		return nil, fmt.Errorf("hwr: unsupported model version %d", v)
	}
	m := &Model{ID: sum}
	m.Labels = append([]byte(nil), r.take(r.u16())...)
	if th := r.f32s(1); th != nil {
		m.Threshold = th[0]
	}
	m.in = [3]int{r.u16(), r.u16(), r.u16()}
	shape := m.in
	m.maxAct = shape[0] * shape[1] * shape[2]
	n := r.u16()
	for i := 0; i < n && r.err == nil; i++ {
		l := layer{op: r.u8(), inShape: shape}
		switch l.op {
		case opConv:
			l.outC, l.inC = r.u16(), r.u16()
			l.k, l.stride, l.pad, l.relu = r.u8(), r.u8(), r.u8(), r.u8() != 0
			if l.inC != shape[0] || l.k == 0 || l.stride == 0 {
				return nil, fmt.Errorf("hwr: layer %d: bad conv shape", i)
			}
			scale := r.f32s(l.outC)
			l.bias = r.f32s(l.outC)
			l.w = r.dequant(scale, l.inC*l.k*l.k)
			ho := (shape[1]+2*l.pad-l.k)/l.stride + 1
			wo := (shape[2]+2*l.pad-l.k)/l.stride + 1
			shape = [3]int{l.outC, ho, wo}
			l.colLen = ho * wo * l.inC * l.k * l.k
			m.maxCol = max(m.maxCol, l.colLen)
		case opMaxPool:
			l.k, l.stride = r.u8(), r.u8()
			if l.k == 0 || l.stride == 0 {
				return nil, fmt.Errorf("hwr: layer %d: bad pool", i)
			}
			shape = [3]int{shape[0], (shape[1]-l.k)/l.stride + 1, (shape[2]-l.k)/l.stride + 1}
		case opDense:
			l.outC, l.inC = r.u16(), r.u16()
			l.relu = r.u8() != 0
			if l.inC != shape[0]*shape[1]*shape[2] {
				return nil, fmt.Errorf("hwr: layer %d: dense input %d != %v", i, l.inC, shape)
			}
			scale := r.f32s(l.outC)
			l.bias = r.f32s(l.outC)
			l.w = r.dequant(scale, l.inC)
			shape = [3]int{l.outC, 1, 1}
		default:
			return nil, fmt.Errorf("hwr: layer %d: unknown op %d", i, l.op)
		}
		l.outShape = shape
		m.maxAct = max(m.maxAct, shape[0]*shape[1]*shape[2])
		m.layers = append(m.layers, l)
	}
	if r.err != nil {
		return nil, fmt.Errorf("hwr: truncated model: %w", r.err)
	}
	if r.off != len(body) {
		return nil, errors.New("hwr: trailing bytes in model")
	}
	if shape[0] != len(m.Labels) || len(m.layers) == 0 {
		return nil, fmt.Errorf("hwr: model outputs %d values for %d labels", shape[0], len(m.Labels))
	}
	m.embedAt = len(m.layers) - 1
	for i := range m.layers {
		if m.layers[i].op == opDense {
			m.embedAt = i
			break
		}
	}
	return m, nil
}

type workspace struct{ a, b, col []float32 }

// Logits runs the network on a 28x28 image and returns one raw score per
// label.
func (m *Model) Logits(img *[RasterSize * RasterSize]float32) []float32 {
	return m.forward(img, nil)
}

// EmbedDim is the length of the vectors Embed returns (576 for the shipped
// model: 64 channels x 3 x 3).
func (m *Model) EmbedDim() int {
	in := m.layers[m.embedAt].inShape
	return in[0] * in[1] * in[2]
}

// Embed rasterises strokes and returns the activations entering the first
// dense layer (the flattened output of the conv stack), L2-normalised, so
// the dot product of two embeddings is their cosine similarity. Glyphs of
// one digit in one hand land close together; Personal builds on this.
//
// These conv features were chosen over the penultimate dense layer because
// they keep more shape detail: on held-out writers, nearest-neighbour
// matching on them lifted more of a player's digits at fewer false accepts
// on letters.
func (m *Model) Embed(strokes []Stroke) []float32 {
	img := Rasterize(strokes)
	e := make([]float32, m.EmbedDim())
	m.forward(&img, e)
	normalize(e)
	return e
}

// normalize scales v to unit L2 norm (a zero vector stays zero).
func normalize(v []float32) {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s <= 0 {
		return
	}
	k := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= k
	}
}

// forward runs the network and returns the logits. If feat is non-nil it
// also receives the (unnormalised) Embed activations, len EmbedDim.
func (m *Model) forward(img *[RasterSize * RasterSize]float32, feat []float32) []float32 {
	ws, _ := m.pool.Get().(*workspace)
	if ws == nil {
		ws = &workspace{make([]float32, m.maxAct), make([]float32, m.maxAct), make([]float32, m.maxCol)}
	}
	cur := ws.a[:len(img)]
	copy(cur, img[:])
	next := ws.b
	for i := range m.layers {
		l := &m.layers[i]
		if feat != nil && i == m.embedAt {
			copy(feat, cur)
		}
		n := l.outShape[0] * l.outShape[1] * l.outShape[2]
		out := next[:n]
		switch l.op {
		case opConv:
			l.conv(cur, out, ws.col[:l.colLen])
		case opMaxPool:
			l.maxPool(cur, out)
		case opDense:
			gemm(l.w, l.bias, cur, out, l.outC, l.inC, 1, l.relu)
		}
		cur, next = out, cur[:cap(cur)]
	}
	res := append([]float32(nil), cur...)
	m.pool.Put(ws)
	return res
}

func (l *layer) conv(in, out, col []float32) {
	c, h, w := l.inShape[0], l.inShape[1], l.inShape[2]
	ho, wo := l.outShape[1], l.outShape[2]
	k, s, pad := l.k, l.stride, l.pad
	kk := c * k * k
	// im2col: one contiguous patch of kk values per output pixel.
	i := 0
	for oy := 0; oy < ho; oy++ {
		for ox := 0; ox < wo; ox++ {
			for ic := 0; ic < c; ic++ {
				plane := in[ic*h*w : (ic+1)*h*w]
				for ky := 0; ky < k; ky++ {
					iy := oy*s - pad + ky
					for kx := 0; kx < k; kx++ {
						ix := ox*s - pad + kx
						if iy >= 0 && iy < h && ix >= 0 && ix < w {
							col[i] = plane[iy*w+ix]
						} else {
							col[i] = 0
						}
						i++
					}
				}
			}
		}
	}
	gemm(l.w, l.bias, col, out, l.outC, kk, ho*wo, l.relu)
}

// gemm computes out[o*np+p] = bias[o] + dot(w[o*kk:], x[p*kk:]) for o<no,
// p<np, optionally followed by ReLU. Blocked 4 outputs x 2 pixels so the
// inner loop does 8 FMAs per 6 loads.
func gemm(w, bias, x, out []float32, no, kk, np int, relu bool) {
	o := 0
	for ; o+4 <= no; o += 4 {
		w0 := w[o*kk : (o+1)*kk]
		w1 := w[(o+1)*kk : (o+2)*kk]
		w2 := w[(o+2)*kk : (o+3)*kk]
		w3 := w[(o+3)*kk : (o+4)*kk]
		p := 0
		for ; p+2 <= np; p += 2 {
			xa := x[p*kk : (p+1)*kk]
			xb := x[(p+1)*kk : (p+2)*kk]
			xb = xb[:len(xa)]
			w0, w1, w2, w3 := w0[:len(xa)], w1[:len(xa)], w2[:len(xa)], w3[:len(xa)]
			var a0, a1, a2, a3, b0, b1, b2, b3 float32
			for i, va := range xa {
				vb := xb[i]
				q0, q1, q2, q3 := w0[i], w1[i], w2[i], w3[i]
				a0 += q0 * va
				a1 += q1 * va
				a2 += q2 * va
				a3 += q3 * va
				b0 += q0 * vb
				b1 += q1 * vb
				b2 += q2 * vb
				b3 += q3 * vb
			}
			put(out, o*np+p, a0+bias[o], relu)
			put(out, (o+1)*np+p, a1+bias[o+1], relu)
			put(out, (o+2)*np+p, a2+bias[o+2], relu)
			put(out, (o+3)*np+p, a3+bias[o+3], relu)
			put(out, o*np+p+1, b0+bias[o], relu)
			put(out, (o+1)*np+p+1, b1+bias[o+1], relu)
			put(out, (o+2)*np+p+1, b2+bias[o+2], relu)
			put(out, (o+3)*np+p+1, b3+bias[o+3], relu)
		}
		for ; p < np; p++ {
			xa := x[p*kk : (p+1)*kk]
			w0, w1, w2, w3 := w0[:len(xa)], w1[:len(xa)], w2[:len(xa)], w3[:len(xa)]
			var a0, a1, a2, a3 float32
			for i, va := range xa {
				a0 += w0[i] * va
				a1 += w1[i] * va
				a2 += w2[i] * va
				a3 += w3[i] * va
			}
			put(out, o*np+p, a0+bias[o], relu)
			put(out, (o+1)*np+p, a1+bias[o+1], relu)
			put(out, (o+2)*np+p, a2+bias[o+2], relu)
			put(out, (o+3)*np+p, a3+bias[o+3], relu)
		}
	}
	for ; o < no; o++ {
		wr := w[o*kk : (o+1)*kk]
		for p := 0; p < np; p++ {
			xa := x[p*kk : (p+1)*kk]
			wr := wr[:len(xa)]
			var a float32
			for i, va := range xa {
				a += wr[i] * va
			}
			put(out, o*np+p, a+bias[o], relu)
		}
	}
}

func put(out []float32, i int, v float32, relu bool) {
	if relu && v < 0 {
		v = 0
	}
	out[i] = v
}

func (l *layer) maxPool(in, out []float32) {
	c, h, w := l.inShape[0], l.inShape[1], l.inShape[2]
	ho, wo := l.outShape[1], l.outShape[2]
	k, s := l.k, l.stride
	for ic := 0; ic < c; ic++ {
		plane := in[ic*h*w : (ic+1)*h*w]
		dst := out[ic*ho*wo : (ic+1)*ho*wo]
		for oy := 0; oy < ho; oy++ {
			for ox := 0; ox < wo; ox++ {
				best := float32(math.Inf(-1))
				for ky := 0; ky < k; ky++ {
					row := plane[(oy*s+ky)*w:]
					for kx := 0; kx < k; kx++ {
						best = max(best, row[ox*s+kx])
					}
				}
				dst[oy*wo+ox] = best
			}
		}
	}
}
