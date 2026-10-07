//go:build ignore

// Benchmark verified tagging on private working copies, never on input files.
package main

import (
	"audiotag/internal/tag"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"time"
)

type result struct {
	File           string             `json:"file"`
	Workload       string             `json:"workload"`
	SizeBytes      int64              `json:"size_bytes"`
	SamplesMS      []float64          `json:"samples_ms"`
	MedianMS       float64            `json:"median_ms"`
	MiBPerSecond   float64            `json:"mib_per_second"`
	AllocatedBytes uint64             `json:"median_allocated_bytes,omitempty"`
	Allocations    uint64             `json:"median_allocations,omitempty"`
	ProgressEvents uint64             `json:"median_progress_events,omitempty"`
	PhasesMS       map[string]float64 `json:"median_phases_ms,omitempty"`
}
type report struct {
	GoVersion string   `json:"go_version"`
	OS        string   `json:"os"`
	Arch      string   `json:"arch"`
	CPUs      int      `json:"cpus"`
	Cache     string   `json:"cache"`
	Results   []result `json:"results"`
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	iterations := flag.Int("iterations", 5, "measured iterations per case")
	warmup := flag.Int("warmup", 1, "unmeasured iterations per case")
	work := flag.String("work", "dist/bench/work", "working-copy directory (choose the filesystem under test)")
	output := flag.String("output", "dist/bench/results.json", "JSON report")
	profile := flag.String("cpuprofile", "", "optional CPU profile")
	cliBinary := flag.String("cli", "", "benchmark CLI rather than in-process API")
	modes := flag.String("modes", "title,book,progress,backup", "comma-separated workloads")
	flag.Parse()
	if *iterations < 1 || *warmup < 0 || len(flag.Args()) == 0 {
		return fmt.Errorf("usage: go run scripts/benchmark.go [options] FIXTURE...")
	}
	if e := os.MkdirAll(*work, 0755); e != nil {
		return e
	}
	// Refuse fixtures in the scratch directory or used as report/profile paths.
	scratch, e := filepath.EvalSymlinks(*work)
	if e != nil {
		return e
	}
	scratch, e = filepath.Abs(scratch)
	if e != nil {
		return e
	}
	for _, input := range flag.Args() {
		resolved, e := filepath.EvalSymlinks(input)
		if e != nil {
			return e
		}
		resolved, e = filepath.Abs(resolved)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(scratch, resolved)
		if e != nil {
			return e
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("fixture must be outside scratch directory: %s", input)
		}
		source, e := os.Stat(resolved)
		if e != nil {
			return e
		}
		for _, dest := range []string{*output, *profile} {
			if dest == "" {
				continue
			}
			absolute, e := filepath.Abs(dest)
			if e != nil {
				return e
			}
			if absolute == resolved {
				return fmt.Errorf("output would overwrite fixture: %s", dest)
			}
			if st, e := os.Stat(dest); e == nil && os.SameFile(source, st) {
				return fmt.Errorf("output aliases fixture: %s", dest)
			}
		}
	}
	if *profile != "" {
		f, e := os.Create(*profile)
		if e != nil {
			return e
		}
		defer f.Close()
		if e = pprof.StartCPUProfile(f); e != nil {
			return e
		}
		defer pprof.StopCPUProfile()
	}
	if *cliBinary != "" {
		p, e := filepath.Abs(*cliBinary)
		if e != nil {
			return e
		}
		*cliBinary = p
	}
	lyrics := strings.Repeat("Chapter 1: Grüße 日本語.\n\nA narrator reads the book.\n", 20000)
	lyricsPath := filepath.Join(*work, "transcript.txt")
	if e := os.WriteFile(lyricsPath, []byte(lyrics), 0644); e != nil {
		return e
	}
	rep := report{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Cache: "warm page cache; restore and GC excluded; Save includes fsync and integrity checks"}
	for _, input := range flag.Args() {
		input, e := filepath.Abs(input)
		if e != nil {
			return e
		}
		st, e := os.Stat(input)
		if e != nil {
			return e
		}
		for _, mode := range strings.Split(*modes, ",") {
			if mode != "title" && mode != "book" && mode != "progress" && mode != "backup" {
				return fmt.Errorf("unknown workload %s", mode)
			}
			path := filepath.Join(*work, "working"+filepath.Ext(input))
			r := result{File: filepath.Base(input), Workload: mode, SizeBytes: st.Size()}
			var allocs, allocated, events []uint64
			phases := map[string][]float64{}
			for iteration := -*warmup; iteration < *iterations; iteration++ {
				if e = restore(input, path); e != nil {
					return e
				}
				os.Remove(path + ".bak")
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				phaseTimes := map[string]float64{}
				lastStage := ""
				lastTime := time.Now()
				var calls uint64
				callback := func(p tag.Progress) {
					calls++
					if p.Stage != lastStage {
						now := time.Now()
						if lastStage != "" {
							phaseTimes[lastStage] += float64(now.Sub(lastTime)) / float64(time.Millisecond)
						}
						lastStage = p.Stage
						lastTime = now
					}
				}
				start := time.Now()
				if *cliBinary != "" {
					args := []string{"set", "--set", "title=Benchmark Part 2"}
					if mode != "title" {
						for _, kv := range bookAssignments() {
							args = append(args, "--set", kv[0]+"="+kv[1])
						}
						args = append(args, "--set-file", "lyrics="+lyricsPath)
					}
					if mode == "progress" {
						args = append(args, "--progress", "always")
					} else {
						args = append(args, "--no-progress")
					}
					if mode == "backup" {
						args = append(args, "--backup")
					}
					args = append(args, path)
					cmd := exec.Command(*cliBinary, args...)
					cmd.Stdout = io.Discard
					cmd.Stderr = io.Discard
					e = cmd.Run()
				} else {
					var f *tag.File
					f, e = tag.Open(path)
					if e == nil {
						e = f.Set("title", []string{"Benchmark Part 2"})
					}
					if e == nil && mode != "title" {
						for _, kv := range bookAssignments() {
							if e = f.Set(kv[0], []string{kv[1]}); e != nil {
								break
							}
						}
						if e == nil {
							e = f.Set("lyrics", []string{lyrics})
						}
					}
					if e == nil {
						if mode == "progress" {
							e = f.SaveWithProgress(false, callback)
						} else {
							e = f.Save(mode == "backup")
						}
					}
					if f != nil {
						f.Close()
					}
				}
				elapsed := time.Since(start)
				if lastStage != "" {
					phaseTimes[lastStage] += float64(time.Since(lastTime)) / float64(time.Millisecond)
				}
				runtime.ReadMemStats(&after)
				if e != nil {
					return fmt.Errorf("%s %s: %w", input, mode, e)
				}
				if iteration >= 0 {
					r.SamplesMS = append(r.SamplesMS, float64(elapsed)/float64(time.Millisecond))
					if *cliBinary == "" {
						allocated = append(allocated, after.TotalAlloc-before.TotalAlloc)
						allocs = append(allocs, after.Mallocs-before.Mallocs)
						events = append(events, calls)
						for stage, ms := range phaseTimes {
							phases[stage] = append(phases[stage], ms)
						}
					}
				}
			}
			r.MedianMS = median(r.SamplesMS)
			r.MiBPerSecond = float64(r.SizeBytes) / (1 << 20) / (r.MedianMS / 1000)
			if *cliBinary == "" {
				r.AllocatedBytes = medianUint(allocated)
				r.Allocations = medianUint(allocs)
				r.ProgressEvents = medianUint(events)
				if len(phases) > 0 {
					r.PhasesMS = map[string]float64{}
					for stage, samples := range phases {
						r.PhasesMS[stage] = median(samples)
					}
				}
			}
			rep.Results = append(rep.Results, r)
			fmt.Printf("%-10s %-9s %8.1f ms  %6.1f MiB/s", r.File, mode, r.MedianMS, r.MiBPerSecond)
			if *cliBinary == "" {
				fmt.Printf("  allocated %.1f MiB", float64(r.AllocatedBytes)/(1<<20))
			}
			fmt.Println()
		}
	}
	if e := os.MkdirAll(filepath.Dir(*output), 0755); e != nil {
		return e
	}
	f, e := os.Create(*output)
	if e != nil {
		return e
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}
func bookAssignments() [][2]string {
	return [][2]string{{"album", "Benchmark Book"}, {"artist", "Example Author"}, {"albumartist", "Example Author"}, {"author", "Example Author"}, {"composer", "Example Narrator"}, {"narrator", "Example Narrator"}, {"genre", "Audiobook"}, {"language", "eng"}, {"tracknumber", "2"}, {"tracktotal", "12"}}
}
func restore(input, output string) error {
	src, e := os.Open(input)
	if e != nil {
		return e
	}
	defer src.Close()
	dst, e := os.Create(output)
	if e != nil {
		return e
	}
	_, e = io.Copy(dst, src)
	closeErr := dst.Close()
	if e == nil {
		e = closeErr
	}
	return e
}
func median(v []float64) float64 {
	x := append([]float64(nil), v...)
	sort.Float64s(x)
	if len(x)%2 == 0 {
		return (x[len(x)/2-1] + x[len(x)/2]) / 2
	}
	return x[len(x)/2]
}
func medianUint(v []uint64) uint64 {
	x := append([]uint64(nil), v...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	return x[len(x)/2]
}
