package main

// Benchmarks nativos (sem dependências). As notas são RELATIVAS: servem para comparar
// o mesmo PC antes e depois dos ajustes, não para comparar com outros computadores.

import (
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	benchScale float64 = 1 // reduz a carga nos testes automáticos
	stopBench  atomic.Bool
	benchMu    sync.Mutex // um benchmark por vez
	sink       atomic.Uint64
)

func scaled(n int) int {
	v := int(float64(n) * benchScale)
	if v < 1000 {
		v = 1000
	}
	return v
}

// cpuWork: mistura de inteiros (xorshift/multiplicação) e ponto flutuante.
func cpuWork(n int, seed uint64) uint64 {
	x := seed | 1
	f := 1.0
	for i := 0; i < n; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		x *= 0x9E3779B97F4A7C15
		f = f*1.0000001 + float64(x&1023)*1e-9
		if f > 2 {
			f -= 1
		}
	}
	return x ^ math.Float64bits(f)
}

func round(v float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(v*p) / p
}

// ---------- CPU 1 núcleo ----------
func BenchSingle() map[string]any {
	benchMu.Lock()
	defer benchMu.Unlock()
	n := scaled(450_000_000)
	best := math.MaxFloat64
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for r := 0; r < 3 && !stopBench.Load(); r++ {
		t := time.Now()
		sink.Add(cpuWork(n, uint64(r+1)))
		if d := time.Since(t).Seconds(); d < best {
			best = d
		}
	}
	mits := float64(n) / best / 1e6
	return map[string]any{"mits": round(mits, 1), "points": int(mits * 10)}
}

// ---------- CPU todos os núcleos ----------
func BenchMulti() map[string]any {
	benchMu.Lock()
	defer benchMu.Unlock()
	threads := runtime.NumCPU()
	n := scaled(300_000_000)
	best := math.MaxFloat64
	for r := 0; r < 3 && !stopBench.Load(); r++ {
		var wg sync.WaitGroup
		t := time.Now()
		for i := 0; i < threads; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sink.Add(cpuWork(n, uint64(i+1)))
			}(i)
		}
		wg.Wait()
		if d := time.Since(t).Seconds(); d < best {
			best = d
		}
	}
	mits := float64(n*threads) / best / 1e6
	return map[string]any{"mits": round(mits, 1), "threads": threads, "points": int(mits * 10)}
}

// ---------- Memória RAM ----------
func BenchRAM() map[string]any {
	benchMu.Lock()
	defer benchMu.Unlock()
	size := scaled(128<<20) &^ 7
	if size < 8<<20 {
		size = 8 << 20
	}
	a := make([]byte, size)
	b := make([]byte, size)
	for i := 0; i < len(a); i += 4096 { // força alocação real das páginas
		a[i] = byte(i)
		b[i] = 1
	}
	best := math.MaxFloat64
	for r := 0; r < 6 && !stopBench.Load(); r++ {
		t := time.Now()
		copy(b, a)
		if d := time.Since(t).Seconds(); d < best {
			best = d
		}
		a, b = b, a
	}
	gbs := float64(size) / best / 1e9

	// latência de acesso aleatório (pointer chasing)
	cnt := scaled(16<<20) &^ 1
	if cnt < 1<<20 {
		cnt = 1 << 20
	}
	next := make([]uint32, cnt)
	perm := make([]uint32, cnt)
	for i := range perm {
		perm[i] = uint32(i)
	}
	s := uint64(88172645463325252)
	for i := cnt - 1; i > 0; i-- { // Sattolo: um único ciclo
		s ^= s << 13
		s ^= s >> 7
		s ^= s << 17
		j := int(s % uint64(i))
		perm[i], perm[j] = perm[j], perm[i]
	}
	for i := 0; i < cnt; i++ {
		next[perm[i]] = perm[(i+1)%cnt]
	}
	steps := scaled(10_000_000)
	p := uint32(0)
	t := time.Now()
	for i := 0; i < steps; i++ {
		p = next[p]
	}
	ns := float64(time.Since(t).Nanoseconds()) / float64(steps)
	sink.Add(uint64(p))

	// nota: banda alta e latência baixa => nota alta
	pts := int(gbs*100 + 20000/ns)
	return map[string]any{"gbs": round(gbs, 2), "latency_ns": round(ns, 1), "points": pts}
}

// ---------- Latência do sistema (travadinhas) ----------
// Um laço fixo mede os "buracos" de tempo: quando outro processo/driver interrompe a CPU.
func BenchLatency() map[string]any {
	benchMu.Lock()
	defer benchMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dur := time.Duration(float64(4*time.Second) * math.Max(benchScale, 0.1))
	var gaps []float64 // µs, só os maiores que 30 µs
	var lost float64
	var n50, n200, n1000 int
	start := time.Now()
	last := start
	for {
		now := time.Now()
		g := now.Sub(last).Seconds() * 1e6
		last = now
		if g > 30 {
			gaps = append(gaps, g)
			lost += g
			if g > 50 {
				n50++
			}
			if g > 200 {
				n200++
			}
			if g > 1000 {
				n1000++
			}
		}
		if now.Sub(start) >= dur || stopBench.Load() {
			break
		}
	}
	total := time.Since(start).Seconds() * 1e6
	sort.Float64s(gaps)
	max := 0.0
	p99 := 0.0
	if len(gaps) > 0 {
		max = gaps[len(gaps)-1]
		p99 = gaps[int(float64(len(gaps)-1)*0.99)]
	}
	lostPct := lost / total * 100

	// precisão do Sleep(1ms)
	var over []float64
	for i := 0; i < 300 && !stopBench.Load(); i++ {
		t := time.Now()
		time.Sleep(time.Millisecond)
		over = append(over, float64(time.Since(t).Microseconds())/1000-1)
	}
	sort.Float64s(over)
	avgO, p99O := 0.0, 0.0
	if len(over) > 0 {
		for _, v := range over {
			avgO += v
		}
		avgO /= float64(len(over))
		p99O = over[int(float64(len(over)-1)*0.99)]
	}
	secs := total / 1e6
	pts := int(1000 / (1 + lostPct*2 + max/2000 + float64(n200)/secs/5))
	return map[string]any{
		"lost_pct": round(lostPct, 3), "max_us": round(max, 0), "p99_us": round(p99, 0),
		"over50": n50, "over200": n200, "over1000": n1000,
		"sleep_avg_ms": round(avgO, 3), "sleep_p99_ms": round(p99O, 3),
		"points": pts,
	}
}

// ---------- Estresse (CPU + RAM) ----------
// Roda todos os núcleos por N segundos e mede a vazão por segundo.
// Queda de desempenho ao longo do tempo indica superaquecimento ou limite de energia.
func BenchStress(seconds int) map[string]any {
	benchMu.Lock()
	defer benchMu.Unlock()
	if seconds < 5 {
		seconds = 5
	}
	if seconds > 600 {
		seconds = 600
	}
	threads := runtime.NumCPU()
	var done atomic.Uint64
	var stop atomic.Bool
	var wg sync.WaitGroup
	chunk := 2_000_000
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := uint64(i + 1)
			for !stop.Load() {
				s = cpuWork(chunk, s)
				done.Add(uint64(chunk))
			}
			sink.Add(s)
		}(i)
	}
	per := make([]float64, 0, seconds)
	prev := uint64(0)
	tick := time.NewTicker(time.Second)
	for i := 0; i < seconds && !stopBench.Load(); i++ {
		<-tick.C
		cur := done.Load()
		per = append(per, round(float64(cur-prev)/1e6, 1))
		prev = cur
	}
	tick.Stop()
	stop.Store(true)
	wg.Wait()

	if len(per) < 3 {
		return map[string]any{"seconds": len(per), "per_second": per, "points": 0, "stopped": true}
	}
	sum, min := 0.0, math.MaxFloat64
	for _, v := range per {
		sum += v
		if v < min {
			min = v
		}
	}
	avg := sum / float64(len(per))
	k := len(per) / 5
	if k < 1 {
		k = 1
	}
	head, tail := 0.0, 0.0
	for i := 0; i < k; i++ {
		head += per[i]
		tail += per[len(per)-1-i]
	}
	head /= float64(k)
	tail /= float64(k)
	drop := (head - tail) / head * 100
	if drop < 0 {
		drop = 0
	}
	stab := min / avg * 100
	return map[string]any{
		"seconds": len(per), "per_second": per, "avg_mits": round(avg, 1), "min_mits": round(min, 1),
		"drop_pct": round(drop, 1), "stability_pct": round(stab, 1),
		"points": int(avg * 10 * (stab / 100)),
	}
}
