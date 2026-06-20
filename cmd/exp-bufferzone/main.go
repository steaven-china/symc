// exp-bufferzone: P4 缓冲带 sim(2026-06-20)
//
// 仿 M5.5 架构:合成数据 + 三数据集切分 + 网格搜索拟合。
// 测"缓冲带宽度 N vs CompositeEvent 减少 vs 算力税"。
//
// Sim 模型:
//   - 8×8 chunks 世界(64 chunks)
//   - 玩家随机移动,跨 region 边界 → 跨区事件
//   - 缓冲带宽度 N: region 边界两侧各 N chunk 内的"跨区"被邻居 region 模拟了,不算跨区事件
//   - 算力税:缓冲带内每 chunk 2× 模拟(primary + neighbor)
//
// 拟合目标:找 N 最小且 CompositeEvent 减少最显著的拐点。
// True N (假设最优)= 2,让 sim 拟合出来。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
)

const (
	WorldSize       = 8          // 8x8 chunks
	NumPlayers      = 10         // 玩家数
	TicksPerSim     = 100        // 每个 sim 跑 100 tick
	NumSims         = 200        // 总 sim 数
	TrueBufferWidth = 2          // 假设最优,sim 拟合目标
	RegionSize      = 4          // 4x4 chunks / region
)

// SimResult 一个 sim 的结果
type SimResult struct {
	SimID             int
	BufferWidth       int
	CompositeEvents   int
	ComputeCost       int // 模拟总成本(chunks × 模拟次数)
	CompositeEventEff int // CompositeEvent × RegionSize(粗略 estimate)
}

func main() {
	seed := flag.Int64("seed", 42, "RNG seed")
	outDir := flag.String("out", "data/bufferzone", "")
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	_ = os.MkdirAll(*outDir, 0o755)

	// 1) 三数据集切分
	allSims := make([][]Player, NumSims)
	for i := 0; i < NumSims; i++ {
		allSims[i] = simulatePlayers(rng, NumPlayers, TicksPerSim)
	}
	nTrain := int(0.6 * float64(NumSims))
	nVal := int(0.2 * float64(NumSims))
	trainSims := allSims[:nTrain]
	valSims := allSims[nTrain : nTrain+nVal]
	testSims := allSims[nTrain+nVal:]

	// 2) 拟合:遍历 N 候选,val 选最优
	candidateN := []int{0, 1, 2, 3, 4}
	type Fitted struct {
		N            int
		TrainAvgCost float64
		ValAvgCost   float64
		TestAvgCost  float64
		ValEvents    float64
	}
	results := []Fitted{}
	for _, n := range candidateN {
		var trainCost, valCost, testCost, valEvents float64
		for _, ps := range trainSims {
			c, e := evaluateSim(ps, n)
			_ = e
			trainCost += float64(c)
		}
		for _, ps := range valSims {
			c, e := evaluateSim(ps, n)
			valCost += float64(c)
			valEvents += float64(e)
		}
		for _, ps := range testSims {
			c, _ := evaluateSim(ps, n)
			testCost += float64(c)
		}
		tn, vn, ten := float64(len(trainSims)), float64(len(valSims)), float64(len(testSims))
		results = append(results, Fitted{
			N:            n,
			TrainAvgCost: trainCost / tn,
			ValAvgCost:   valCost / vn,
			TestAvgCost:  testCost / ten,
			ValEvents:    valEvents / vn,
		})
		fmt.Printf("N=%-2d train_cost=%.1f val_cost=%.1f test_cost=%.1f val_events=%.1f\n",
			n, trainCost/tn, valCost/vn, testCost/ten, valEvents/vn)
	}

	// 选 N 最小且 val_events 减少量最大的
	// 简化:val_events 最小 = 最优
	var bestN int = -1
	bestEvents := 1e18
	for _, r := range results {
		if r.ValEvents < bestEvents {
			bestEvents = r.ValEvents
			bestN = r.N
		}
	}

	// 3) 三指标(类似 M5.5)
	// 简化:只算"拟合 N vs True N"差距
	gap := abs(bestN - TrueBufferWidth)

	out := map[string]any{
		"results":             results,
		"true_buffer_width":   TrueBufferWidth,
		"fitted_buffer_width": bestN,
		"abs_gap":             gap,
		"note":                "P4 buffer zone sim. synthetic data. true N=2.",
		"seed":                *seed,
	}
	f, _ := os.Create(filepath.Join(*outDir, "fit.json"))
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	fmt.Printf("\nTrue N=%d  Fitted N=%d  Gap=%d\n", TrueBufferWidth, bestN, gap)
}

// Player 一个玩家的轨迹
type Player struct {
	X, Z int // chunk 坐标
}

func simulatePlayers(rng *rand.Rand, numPlayers, ticks int) []Player {
	// 每个 sim:返回 numPlayers 玩家,ticks 个位置快照(简化:每个玩家一个最终位置)
	// 实际:我们应该存轨迹,这里简化只存最终位置
	players := make([]Player, numPlayers)
	for i := range players {
		// 起始位置 + ticks 步随机游走
		x, z := rng.Intn(WorldSize), rng.Intn(WorldSize)
		for t := 0; t < ticks; t++ {
			switch rng.Intn(4) {
			case 0:
				x = (x + 1) % WorldSize
			case 1:
				x = (x - 1 + WorldSize) % WorldSize
			case 2:
				z = (z + 1) % WorldSize
			case 3:
				z = (z - 1 + WorldSize) % WorldSize
			}
		}
		players[i] = Player{X: x, Z: z}
	}
	_ = sort.Slice // 占位,避免 unused
	return players
}

// evaluateSim 给定玩家位置 + 缓冲带宽度 N,算 (compute_cost, composite_events)
func evaluateSim(players []Player, bufferN int) (int, int) {
	// Compute cost: 缓冲带外每 chunk 1 次模拟,缓冲带内 2 次
	// 简化: 总 chunks × (1 + 2N) 但受 region 边界比例影响
	// 真实计算:每 region 边界长度 × N × 2 + 非边界 chunks
	totalChunks := WorldSize * WorldSize
	bufferChunks := (WorldSize * 2) * bufferN // 4 边界 × WorldSize × N
	if bufferChunks > totalChunks {
		bufferChunks = totalChunks
	}
	nonBuffer := totalChunks - bufferChunks
	cost := nonBuffer*1 + bufferChunks*2

	// Composite events: 跨 region 边界的玩家数
	// 缓冲带 N 让"刚跨过 N 以内的玩家"不算跨区
	events := 0
	for _, p := range players {
		distToBoundary := distToRegionBoundary(p.X, p.Z)
		if distToBoundary < RegionSize/2 && distToBoundary >= bufferN {
			events++
		}
	}
	return cost, events
}

func regionOf(x, z int) (rx, rz int) {
	return x / RegionSize, z / RegionSize
}

// distToRegionBoundary 玩家 (x,z) 到最近 region 边界的距离
func distToRegionBoundary(x, z int) int {
	// 在 region 内的相对位置
	relX := x % RegionSize
	relZ := z % RegionSize
	// 到 4 条 region 内边界的最小距离
	dx := relX
	if RegionSize-1-relX < dx {
		dx = RegionSize - 1 - relX
	}
	dz := relZ
	if RegionSize-1-relZ < dz {
		dz = RegionSize - 1 - relZ
	}
	if dx < dz {
		return dx
	}
	return dz
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
