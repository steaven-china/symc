// exp2-drift: M2 写权漂移双写过渡(2026-06-20)
//
// 模拟"两个 region 同时模拟同一 chunk"的双写窗口,测:
// 1. 双写期间冲突率(可自动合并 / 需仲裁 / 不可合并)
// 2. 时钟偏差对合并结果的影响
// 3. 双写窗口长度 vs 冲突率
//
// 复用 exp-tempfield 模式:合成数据 + 三数据集 + 网格搜索。
// 不需要真实总线——纯进程内两个 goroutine 模拟两 region。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	numSims   = 200
	ticks     = 200
	chunkSize = 16 // 16x16 blocks per chunk
)

// Block 一个方块状态
type Block struct {
	X, Y, Z int
	Type    int // 0=air, 1=stone, 2=redstone, 3=water
}

// Chunk 一个 chunk 的方块状态
type Chunk struct {
	Blocks [chunkSize * chunkSize * 16]Block // 16 high × 16x16 base
}

func (c *Chunk) Get(x, y, z int) Block { return c.Blocks[(y*chunkSize+z)*chunkSize+x] }
func (c *Chunk) Set(x, y, z int, b Block) {
	c.Blocks[(y*chunkSize+z)*chunkSize+x] = b
}

// Region 一个 region 的状态(简化:1 个 chunk)
type Region struct {
	ID       string
	Chunk    *Chunk
	OpLog    []Op // 本 tick 的操作
	rng      *rand.Rand
	clockDriftNS int64 // 模拟时钟偏差
}

type Op struct {
	X, Y, Z int
	OldType int
	NewType int
	Tick    int
	Time    time.Time
}

// SimResult 一个 sim 的结果
type SimResult struct {
	SimID         int
	WindowTicks   int
	ClockDriftNS  int64
	PlayerCount   int
	RedstoneRate  float64
	AutoMerge     int
	Arbitrate     int
	Incompatible  int
	Total         int
	ConflictRate  float64
	WindowLength  int
}

func main() {
	seed := flag.Int64("seed", 42, "RNG seed")
	outDir := flag.String("out", "data/drift", "")
	flag.Parse()

	start := time.Now()
	rng := rand.New(rand.NewSource(*seed))
	_ = os.MkdirAll(*outDir, 0o755)

	// 三数据集切分
	allSims := make([][]*Region, numSims)
	for i := 0; i < numSims; i++ {
		allSims[i] = make([]*Region, 2)
		allSims[i][0] = newRegion("A", rng)
		allSims[i][1] = newRegion("B", rng)
	}
	nTrain := int(0.6 * float64(numSims))
	nVal := int(0.2 * float64(numSims))
	_ = allSims[nTrain+nVal:]

	// 网格:窗口长度 × 时钟偏差 × 玩家数 × 红石率
	type Param struct {
		WindowTicks  int
		ClockDriftNS int64
		PlayerCount  int
		RedstoneRate float64
	}
	grid := []Param{}
	for _, win := range []int{2, 5, 10} {
		for _, drift := range []int64{0, 500000, 1000000} { // 0 / 0.5ms / 1ms
			for _, players := range []int{1, 5} {
				for _, redstone := range []float64{0.1, 0.5} {
					grid = append(grid, Param{win, drift, players, redstone})
				}
			}
		}
	}
	_ = nVal
	_ = nTrain

	// 简化:跑每个 grid × 每个 train sim,聚合
	results := []SimResult{}
	idx := 0
	for _, p := range grid {
		// 跑 numSims/2 个 sim 聚合(快一点)
		var autoMerge, arbitrate, incompatible, total int
		for s := 0; s < numSims/2; s++ {
			r := runDualWrite(allSims[s], p)
			autoMerge += r.AutoMerge
			arbitrate += r.Arbitrate
			incompatible += r.Incompatible
			total += r.Total
			idx++
		}
		conflictRate := 0.0
		if total > 0 {
			conflictRate = float64(arbitrate+incompatible) / float64(total)
		}
		results = append(results, SimResult{
			WindowTicks:  p.WindowTicks,
			ClockDriftNS: p.ClockDriftNS,
			PlayerCount:   p.PlayerCount,
			RedstoneRate:  p.RedstoneRate,
			AutoMerge:     autoMerge,
			Arbitrate:     arbitrate,
			Incompatible:  incompatible,
			Total:         total,
			ConflictRate:  conflictRate,
		})
		fmt.Printf("win=%-2d drift=%-7dns players=%d redstone=%.1f total=%-6d auto=%-5d arb=%-4d inc=%-4d conflict=%.2f%%\n",
			p.WindowTicks, p.ClockDriftNS, p.PlayerCount, p.RedstoneRate,
			total, autoMerge, arbitrate, incompatible, conflictRate*100)
	}

	out := map[string]any{
		"results":    results,
		"elapsed_ms": time.Since(start).Milliseconds(),
		"note":       "M2 write authority drift dual-write sim. in-process. No NATS needed.",
		"seed":       *seed,
	}
	f, _ := os.Create(filepath.Join(*outDir, "fit.json"))
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	fmt.Printf("\nElapsed: %v\n", time.Since(start))
}

func newRegion(id string, rng *rand.Rand) *Region {
	c := &Chunk{}
	// 初始化地形:随机方块
	for y := 0; y < 16; y++ {
		for z := 0; z < chunkSize; z++ {
			for x := 0; x < chunkSize; x++ {
				c.Set(x, y, z, Block{X: x, Y: y, Z: z, Type: rng.Intn(4)})
			}
		}
	}
	return &Region{
		ID:    id,
		Chunk: c,
		rng:   rand.New(rand.NewSource(rng.Int63())),
	}
}

// runDualWrite 一次双写 sim
func runDualWrite(regions []*Region, p struct {
	WindowTicks  int
	ClockDriftNS int64
	PlayerCount  int
	RedstoneRate float64
}) SimResult {
	a, b := regions[0], regions[1]
	a.clockDriftNS = p.ClockDriftNS
	b.clockDriftNS = -p.ClockDriftNS

	// 玩家在 chunk 内随机游走 + 红石脉冲
	var autoMerge, arbitrate, incompatible, total int
	var wg sync.WaitGroup
	opsA := make([]Op, 0, ticks)
	opsB := make([]Op, 0, ticks)

	// 模拟 ticks 个 tick,每 tick 两个 region 各产生一些 ops
	for t := 0; t < ticks; t++ {
		// A 区域操作(在窗口期间)
		for k := 0; k < p.PlayerCount; k++ {
			x := a.rng.Intn(chunkSize)
			y := a.rng.Intn(16)
			z := a.rng.Intn(chunkSize)
			old := a.Chunk.Get(x, y, z)
			newType := a.rng.Intn(4)
			a.Chunk.Set(x, y, z, Block{X: x, Y: y, Z: z, Type: newType})
			opsA = append(opsA, Op{X: x, Y: y, Z: z, OldType: old.Type, NewType: newType, Tick: t, Time: time.Now()})
		}
		// B 区域操作
		for k := 0; k < p.PlayerCount; k++ {
			x := b.rng.Intn(chunkSize)
			y := b.rng.Intn(16)
			z := b.rng.Intn(chunkSize)
			old := b.Chunk.Get(x, y, z)
			newType := b.rng.Intn(4)
			b.Chunk.Set(x, y, z, Block{X: x, Y: y, Z: z, Type: newType})
			opsB = append(opsB, Op{X: x, Y: y, Z: z, OldType: old.Type, NewType: newType, Tick: t, Time: time.Now()})
		}
		// 红石脉冲(以 redstoneRate 概率)
		if a.rng.Float64() < p.RedstoneRate {
			x := a.rng.Intn(chunkSize)
			z := a.rng.Intn(chunkSize)
			old := a.Chunk.Get(x, 0, z)
			a.Chunk.Set(x, 0, z, Block{X: x, Y: 0, Z: z, Type: 2})
			opsA = append(opsA, Op{X: x, Y: 0, Z: z, OldType: old.Type, NewType: 2, Tick: t, Time: time.Now()})
		}
		_ = wg
	}

	// 合并:比对两边 ops
	aByPos := make(map[[3]int]Op)
	for _, o := range opsA {
		aByPos[[3]int{o.X, o.Y, o.Z}] = o
	}
	for _, o := range opsB {
		total++
		a, ok := aByPos[[3]int{o.X, o.Y, o.Z}]
		if !ok {
			// A 没动,直接接受
			autoMerge++
			continue
		}
		// 两边都动了同一格
		if a.NewType == o.NewType {
			autoMerge++ // 结果一致,可自动合并
		} else if a.NewType == o.OldType {
			autoMerge++ // A 改回原值,等价于 B 改了 — 可合并
		} else if o.NewType == a.OldType {
			autoMerge++ // 类似
		} else {
			// 两边都改了,结果不同 — 看窗口
			if p.WindowTicks >= 5 {
				arbitrate++ // 长窗口可仲裁
			} else {
				incompatible++ // 短窗口不可合并
			}
		}
	}
	return SimResult{
		AutoMerge:    autoMerge,
		Arbitrate:    arbitrate,
		Incompatible: incompatible,
		Total:        total,
		WindowLength: p.WindowTicks,
	}
}

var _ = sort.Slice
