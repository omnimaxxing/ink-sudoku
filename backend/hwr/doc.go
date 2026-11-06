// Package hwr recognises handwritten sudoku ink on the tablet: big answer
// digits, small pencil-mark candidates, and zig-zag scratch-outs. It is pure
// Go (stdlib only), fully offline, and sized for a Cortex-A53.
//
// # API
//
//	marks := hwr.Analyze(cellRect, strokes)          // embedded model, DefaultConfig
//	a := hwr.Analyzer{Classifier: hwr.DefaultModel(), Config: cfg}
//	marks = a.Analyze(cellRect, strokes)
//	img := hwr.Rasterize(strokes)                    // [784]float32, MNIST-style
//	res := hwr.DefaultModel().Classify(strokes, 0)   // one glyph -> digit/reject
//	m, err := hwr.LoadModel(file)                    // e.g. a per-player fine-tune
//	p := hwr.NewPersonal(m, samples)                 // on-device personalization
//	a = hwr.Analyzer{Classifier: p, Config: cfg}     // p.Add(sample) as corrections arrive
//
// Each Mark has a Kind (Answer, Candidate, ScratchOut, Unknown), Digit 1..9,
// Confidence, Top3, BBox, Strokes (indices into the input) and, for
// scratch-outs, Erases (indices of the strokes it rubs out).
//
// # Pipeline
//
// Analyze is geometric first, ML second. Scratch-outs are found per stroke
// from shape alone (sharp reversals and path length vs bbox diagonal,
// measured after arc-length resampling so the digitiser rate does not
// matter). The rest of the ink is grouped by closest-ink distance with a
// pause-dependent limit, so a 4 written in two strokes or a 5 with a lifted
// top bar is one mark while neighbouring pencil marks stay separate.
// Answer vs candidate is decided by mark height relative to the cell (and
// centredness in the ambiguous band), never by the classifier. Each group is
// then rasterised and classified; a rejected one-stroke group that is a
// straight, near-vertical line of reasonable length is read as 1 (the CNN
// scores a bare line just under its threshold). All thresholds live in
// Config.
//
// The classifier is a small CNN (3x conv3x3+pool, 2 dense) with 11 outputs:
// digits 0-9 and an explicit reject class trained on letters, symbols,
// scribbles, scratch-outs and mis-grouped digit pairs. A glyph is accepted
// only when the arg-max is 1..9 with probability >= the threshold stored in
// model.bin (calibrated on held-out writers); 0 and reject both mean
// Unknown, because sudoku has no 0.
//
// # Train/inference parity
//
// The image the network sees must be produced identically in training and
// on the tablet, so there is exactly one rasteriser specification,
// implemented twice: raster.go here and hwrtrain/src/hwrtrain/raster.py.
// Every stroke-sourced training image (pendigits, UJI Pen Characters,
// synthetic negatives, and MNIST/EMNIST bitmaps re-drawn as skeleton
// strokes) goes through raster.py; every tablet glyph goes through
// raster.go. The two are kept honest by testdata/parity.bin.gz, exported by
// hwrtrain's export.py from held-out writers: TestParity re-rasterises the
// exported strokes in Go, compares the raster against Python's, runs the Go
// network and checks top-1 against PyTorch. The PyTorch reference is the
// network rebuilt from model.bin itself (dequantised int8 weights, BN
// folded), so the test checks exactly what ships. Change a raster constant
// on one side only and TestParity fails.
//
// # Model file
//
// model.bin (embedded with go:embed) is documented in model.go: a small
// header (labels, calibrated threshold, input shape), a layer list with
// per-output-channel int8 weights and float32 scales/biases, and a CRC32.
// Weights are dequantised once at load; inference is float32 scalar Go,
// which on an ARMv8.0 A53 is as fast as int8 would be without SDOT.
//
// # Self-improvement
//
// When the player corrects a misread, the app appends a Sample (see
// sample.go for the JSONL format) via NewSample + AppendSample.
//
// On the tablet, Personal (personal.go) uses those samples (and a
// calibration page) directly, without retraining: it compares each glyph's
// Embed vector with the player's samples and blends nearest-sample
// evidence into the CNN's probabilities before the accept rule. Adding a
// sample costs one CNN pass; a read costs one dot product per sample on
// top of Classify. EvaluatePersonal reports leave-one-out accuracy over a
// player's samples.
//
// Off the tablet, hwrtrain's finetune.py adapts the network itself from
// the same logs and writes a new model.bin that LoadModel can use in place
// of the embedded one (Personal works on top of either).
package hwr
