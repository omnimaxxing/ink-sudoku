// Command pencheck records the stylus for a few seconds, alongside xochitl,
// and prints what the digitizer reports: ranges, sample rate while touching,
// pressure/tilt seen, and the coordinate extremes (for screen mapping).
//
//	pencheck [-s seconds]
package main

import (
	"flag"
	"fmt"
	"log"
	"slices"
	"time"

	"sudoku/pen"
)

func main() {
	secs := flag.Int("s", 15, "seconds to record")
	flag.Parse()

	dev, err := pen.Open()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("device %q at %s\nranges x %v  y %v  pressure %v\n", dev.Name, dev.Path, dev.Ranges.X, dev.Ranges.Y, dev.Ranges.Pressure)
	fmt.Printf("recording %d s: write and draw with the pen now...\n", *secs)

	var (
		events                     []pen.Event
		lagUs                      []int64 // kernel timestamp -> this process reading it
		minX, minY                 int32   = 1 << 30, 1 << 30
		maxX, maxY, maxP           int32
		minTX, maxTX, minTY, maxTY int32
	)
	done := time.After(time.Duration(*secs) * time.Second)
	ch := make(chan pen.Event, 4096)
	go dev.Run(func(e pen.Event) { ch <- e })
loop:
	for {
		select {
		case <-done:
			break loop
		case e := <-ch:
			lagUs = append(lagUs, time.Now().UnixMicro()-e.Time)
			events = append(events, e)
			if e.Kind == pen.Down || e.Kind == pen.Move {
				minX, maxX = min(minX, e.X), max(maxX, e.X)
				minY, maxY = min(minY, e.Y), max(maxY, e.Y)
				maxP = max(maxP, e.Pressure)
			}
			minTX, maxTX = min(minTX, e.TiltX), max(maxTX, e.TiltX)
			minTY, maxTY = min(minTY, e.TiltY), max(maxTY, e.TiltY)
		}
	}

	counts := map[pen.Kind]int{}
	var gaps []int64 // µs between consecutive samples within a stroke
	for i, e := range events {
		counts[e.Kind]++
		if i > 0 && e.Kind == pen.Move && (events[i-1].Kind == pen.Move || events[i-1].Kind == pen.Down) {
			gaps = append(gaps, e.Time-events[i-1].Time)
		}
	}
	fmt.Printf("\nevents %d  enter %d down %d move %d up %d leave %d hover %d\n",
		len(events), counts[pen.Enter], counts[pen.Down], counts[pen.Move], counts[pen.Up], counts[pen.Leave], counts[pen.Hover])
	if len(gaps) > 0 {
		slices.Sort(gaps)
		med := gaps[len(gaps)/2]
		fmt.Printf("touching: sample gap median %d µs (~%.0f Hz), p5 %d µs, p95 %d µs\n",
			med, 1e6/float64(med), gaps[len(gaps)*5/100], gaps[len(gaps)*95/100])
		fmt.Printf("seen while touching: x %d..%d  y %d..%d  max pressure %d\n", minX, maxX, minY, maxY, maxP)
		fmt.Printf("tilt seen: x %d..%d  y %d..%d\n", minTX, maxTX, minTY, maxTY)
	}
	if len(lagUs) > 0 {
		slices.Sort(lagUs)
		fmt.Printf("kernel->reader lag: median %d µs, p95 %d µs (also checks the evdev clock is realtime)\n",
			lagUs[len(lagUs)/2], lagUs[len(lagUs)*95/100])
	}
}
