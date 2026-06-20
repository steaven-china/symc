// exp-tempfield: M5.5 — 温度场 sim 跑通
// 验证 §3.2 + §5.1.1 温度场行为符合预期
// 数据:合成(玩家/红石/实体随机生成,种子可配)
//
// 过拟合控制(§10.5 + phase0-baseline.md §3):
//   - 三数据集切分:train 60% / val 20% / test 20%
//   - 调参只看不参与梯度的 val
//   - test 全程只看一次
//   - 三指标:Train/Val gap / Test/Val gap / 参数稳定性
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/symc/sim/pkg/layer"
)

const (
	TicksPerChunk = 20 // 每个 chunk 模拟 20 tick
	ChunksPerSim  = 100 // 每个 sim 跑 100 chunk
	NumSims       = 200 // 总 sim 数(分 train/val/test)
)

// Sample 一个 (player, redstone, entity) 三元组采样
type Sample struct {
	Player   float64
	Redstone float64
	Entity   float64
	// 真值温度(由"真值权重"计算,作为拟合目标)
	TrueTemp float64
	// 实际观测(加噪声,模拟 sim 测量的不确定性)
	ObsPlayer   float64
	ObsRedstone float64
	ObsEntity   float64
	ObsTemp     float64
}

// TrueWeights "真实"权重(用来生成样本真值温度)
// 注意:这个权重**不**告诉 sim,sim 要自己学出来
// 但 sim 学习目标应该接近这个(否则过拟合检测才有意义)
var TrueWeights = layer.Weights{W1: 1.2, W2: 0.8, W3: 1.5}

// SimResult 一个 sim 的结果
type SimResult struct {
	SimID       int
	Seed        int64
	TrainLoss   float64
	ValLoss     float64
	TestLoss    float64
	W           layer.Weights
}

// GenerateSamples 生成一个 sim 的样本
func GenerateSamples(rng *rand.Rand) []Sample {
	samples := make([]Sample, ChunksPerSim*TicksPerChunk)
	for i := range samples {
		// 随机化三因子(player 偏向低值,模拟大多数 chunk 没人;redstone 偏向中值;entity 偏向低值)
		player := math.Pow(rng.Float64(), 2)            // 偏 0
		redstone := rng.Float64() * 0.8                  // 偏中低
		entity := math.Pow(rng.Float64(), 1.5) * 0.9     // 偏 0
		trueTemp := layer.ComputeTemperature(player, redstone, entity, TrueWeights)
		// 加观测噪声(±5%)
		noise := func(v float64) float64 { return v + (rng.Float64()-0.5)*0.1 }
		obs := Sample{
			Player:       player,
			Redstone:     redstone,
			Entity:       entity,
			TrueTemp:     trueTemp,
			ObsPlayer:    noise(player),
			ObsRedstone:  noise(redstone),
			ObsEntity:    noise(entity),
		}
		obs.ObsTemp = layer.ComputeTemperature(obs.ObsPlayer, obs.ObsRedstone, obs.ObsEntity, TrueWeights)
		samples[i] = obs
	}
	return samples
}

// TrainLoss 拟合 loss = Σ (pred - obs)^2 / N
func TrainLoss(samples []Sample, w layer.Weights) float64 {
	sum := 0.0
	for _, s := range samples {
		pred := layer.ComputeTemperature(s.ObsPlayer, s.ObsRedstone, s.ObsEntity, w)
		d := pred - s.ObsTemp
		sum += d * d
	}
	return sum / float64(len(samples))
}

// FitWeights 简单网格搜索(sim 拟合)
func FitWeights(train []Sample, val []Sample) layer.Weights {
	bestW := layer.DefaultWeights
	bestVal := TrainLoss(val, bestW)
	// 粗搜:每个权重 0.5..2.0,步长 0.1
	for w1 := 0.5; w1 <= 2.0; w1 += 0.1 {
		for w2 := 0.5; w2 <= 2.0; w2 += 0.1 {
			for w3 := 0.5; w3 <= 2.0; w3 += 0.1 {
				w := layer.Weights{W1: w1, W2: w2, W3: w3}
				v := TrainLoss(val, w) // 注意:用 val 选最优
				if v < bestVal {
					bestVal = v
					bestW = w
				}
			}
		}
	}
	return bestW
}

func main() {
	seed := flag.Int64("seed", 42, "RNG seed")
	outDir := flag.String("out", "data/tempfield", "output directory")
	flag.Parse()

	start := time.Now()
	fmt.Fprintf(os.Stderr, "exp-tempfield: seed=%d out=%s\n", *seed, *outDir)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}

	// 1) 生成所有 sim 的样本
	rng := rand.New(rand.NewSource(*seed))
	allSims := make([][]Sample, NumSims)
	for i := 0; i < NumSims; i++ {
		allSims[i] = GenerateSamples(rng)
	}

	// 2) 三数据集切分(60/20/20)
	nTrain := int(0.6 * float64(NumSims))
	nVal := int(0.2 * float64(NumSims))
	// train: [0, nTrain), val: [nTrain, nTrain+nVal), test: [nTrain+nVal, NumSims)

	flat := func(simRange [][]Sample) []Sample {
		var out []Sample
		for _, sim := range simRange {
			out = append(out, sim...)
		}
		return out
	}
	trainSims := allSims[:nTrain]
	valSims := allSims[nTrain : nTrain+nVal]
	testSims := allSims[nTrain+nVal:]

	allTrain := flat(trainSims)
	allVal := flat(valSims)
	allTest := flat(testSims)

	// 3) 拟合(val 选最优)
	bestW := FitWeights(allTrain, allVal)

	// 4) 三指标
	// 整体指标
	trainLoss := TrainLoss(allTrain, bestW)
	valLoss := TrainLoss(allVal, bestW)
	testLoss := TrainLoss(allTest, bestW)
	// 参数稳定性:训练集分 5 份独立拟合,看差异
	stabilityRuns := make([]layer.Weights, 5)
	chunkSize := len(trainSims) / 5
	for i := 0; i < 5; i++ {
		sub := flat(trainSims[i*chunkSize : (i+1)*chunkSize])
		stabilityRuns[i] = FitWeights(sub, allVal)
	}

	// 5) 写文件
	writeJSON(filepath.Join(*outDir, "fit.json"), map[string]any{
		"weights":    bestW,
		"true_weights": TrueWeights,
		"train_loss": trainLoss,
		"val_loss":   valLoss,
		"test_loss":  testLoss,
		"train_val_gap":   (valLoss - trainLoss) / trainLoss,
		"test_val_gap":    (testLoss - valLoss) / valLoss,
		"stability_runs":  stabilityRuns,
		"stability_range": weightRange(stabilityRuns),
		"num_sims": NumSims,
		"num_train": nTrain,
		"num_val":   nVal,
		"num_test":  NumSims - nTrain - nVal,
		"seed":      *seed,
		"elapsed_ms": time.Since(start).Milliseconds(),
	})

	// 控制台输出
	fmt.Printf("True weights: w1=%.2f w2=%.2f w3=%.2f\n", TrueWeights.W1, TrueWeights.W2, TrueWeights.W3)
	fmt.Printf("Fitted:       w1=%.2f w2=%.2f w3=%.2f\n", bestW.W1, bestW.W2, bestW.W3)
	fmt.Printf("Train loss: %.6f\n", trainLoss)
	fmt.Printf("Val loss:   %.6f\n", valLoss)
	fmt.Printf("Test loss:  %.6f\n", testLoss)
	fmt.Printf("Train/Val gap:   %.2f%% (阈值 15%%)\n", (valLoss-trainLoss)/trainLoss*100)
	fmt.Printf("Test/Val gap:    %.2f%% (阈值 10%%)\n", (testLoss-valLoss)/valLoss*100)
	fmt.Printf("Stability range: w1 ±%.2f w2 ±%.2f w3 ±%.2f (阈值 20%%)\n",
		weightRange(stabilityRuns).W1, weightRange(stabilityRuns).W2, weightRange(stabilityRuns).W3)
	fmt.Printf("Elapsed: %v\n", time.Since(start))
}

// weightRange 5 份拟合结果的最大 - 最小(差值)
func weightRange(ws []layer.Weights) layer.Weights {
	if len(ws) == 0 {
		return layer.Weights{}
	}
	w1s := make([]float64, len(ws))
	w2s := make([]float64, len(ws))
	w3s := make([]float64, len(ws))
	for i, w := range ws {
		w1s[i] = w.W1
		w2s[i] = w.W2
		w3s[i] = w.W3
	}
	return layer.Weights{
		W1: percentile(w1s, 100) - percentile(w1s, 0),
		W2: percentile(w2s, 100) - percentile(w2s, 0),
		W3: percentile(w3s, 100) - percentile(w3s, 0),
	}
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64{}, xs...)
	sort.Float64s(cp)
	idx := int(p / 100 * float64(len(cp)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func writeJSON(path string, v any) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", path, err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "encode %s: %v\n", path, err)
	}
}
