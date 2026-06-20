// Package layer · 温度场扩展(2026-06-20,§3.2 + §5.1.1)
//
// 把每个 Cell 的"层"从离散 4 档位扩展为连续温度 T ∈ [0, 1]。
// 三个归约因子:player_proximity / redstone_activity / entity_density。
// 归约函数: T = tanh(w1·P + w2·R + w3·E)
//
// Hysteresis 滞后带(升温阈值 0.6,降温阈值 0.4)避免温变震荡。
// 温变率限幅 ±0.2/tick 避免单次脉冲导致瞬跳。
// 采样稀疏:非订阅 chunk 温度 = 上次值 × 0.9/tick 衰减。
package layer

import (
	"math"
	"sync"
)

// Weights 归约权重(初版 1.0/1.0/1.0,实验 2 校准)
type Weights struct {
	W1 float64 // player_proximity
	W2 float64 // redstone_activity
	W3 float64 // entity_density
}

// DefaultWeights 初版权重(三因子等权,无信息先验,不系统过拟合)
var DefaultWeights = Weights{W1: 1.0, W2: 1.0, W3: 1.0}

// Thresholds hysteresis 滞后带
const (
	TempHigh = 0.6 // 升温到 Dynamic 阈值
	TempLow  = 0.4 // 降温到 Semi-static 阈值
	TempDrop = 0.1 // Ephemeral 可丢阈值
	MaxRate  = 0.2 // 温变率限幅 ±/tick
	Decay    = 0.9 // 非订阅 chunk 衰减系数
)

// ComputeTemperature 计算 cell 温度(三因子归一化输入,weight 加权,tanh)
//
// 参数:
//   - player: 玩家距 chunk 距离 / 视锥半径(0=在 chunk 内,1=刚好视锥边缘)
//   - redstone: 红石变更率 / 最大变更率
//   - entity: 生物+物品+投射物数 / 最大数
//   - w: 归约权重
//
// 返回: T ∈ [0, 1]
func ComputeTemperature(player, redstone, entity float64, w Weights) float64 {
	if player < 0 {
		player = 0
	}
	if player > 1 {
		player = 1
	}
	if redstone < 0 {
		redstone = 0
	}
	if redstone > 1 {
		redstone = 1
	}
	if entity < 0 {
		entity = 0
	}
	if entity > 1 {
		entity = 1
	}
	x := w.W1*player + w.W2*redstone + w.W3*entity
	t := math.Tanh(x)
	if t < 0 {
		t = 0
	}
	return t
}

// ApplyRateLimit 温变率限幅 ±MaxRate/tick
//
// prev 是上 tick 温度,new 是本 tick 计算温度,返回限幅后的温度。
func ApplyRateLimit(prev, newT float64) float64 {
	diff := newT - prev
	if diff > MaxRate {
		return prev + MaxRate
	}
	if diff < -MaxRate {
		return prev - MaxRate
	}
	return newT
}

// ApplyDecay 非订阅 chunk 温度衰减
//
// chunk 不在订阅视锥内时调用,温度指数衰减。
func ApplyDecay(prev float64) float64 {
	return prev * Decay
}

// ClassifyByTemp 按温度 + hysteresis 离散化到 Layer
//
// 用 hysteresis 避免温度在阈值附近震荡:
//
//
//	升温时: T > TempHigh → Dynamic
//	降温时: T < TempLow → Semi-static
//	维持上一 tick 状态(否则不切换)
//
// prevLayer 是上 tick 的离散 Layer,返回本 tick 应该的 Layer。
func ClassifyByTemp(t float64, prevLayer Layer) Layer {
	switch prevLayer {
	case Static, SemiStatic:
		// 升级:温度 > TempHigh → Dynamic
		if t > TempHigh {
			return Dynamic
		}
		// 维持 Semi-static
		if t > TempLow {
			return SemiStatic
		}
		// 降到 Ephemeral
		if t < TempDrop {
			return Ephemeral
		}
		return SemiStatic
	case Dynamic:
		// 降级:温度 < TempLow → Semi-static
		if t < TempLow {
			return SemiStatic
		}
		// 维持 Dynamic
		return Dynamic
	case Ephemeral:
		// 升级:温度 > TempDrop → Semi-static
		if t > TempDrop {
			return SemiStatic
		}
		return Ephemeral
	}
	return SemiStatic
}

// TempField 一个 region 内所有 chunk 的温度缓存
type TempField struct {
	mu     sync.RWMutex
	values map[uint64]float64 // chunkHash → temperature
}

// NewTempField 创建温度场
func NewTempField() *TempField {
	return &TempField{values: make(map[uint64]float64)}
}

// Get 读温度
func (tf *TempField) Get(chunkHash uint64) float64 {
	tf.mu.RLock()
	defer tf.mu.RUnlock()
	return tf.values[chunkHash]
}

// Set 写温度
func (tf *TempField) Set(chunkHash uint64, t float64) {
	tf.mu.Lock()
	defer tf.mu.Unlock()
	tf.values[chunkHash] = t
}

// Update 更新温度(限幅 + 写回)
func (tf *TempField) Update(chunkHash uint64, rawTemp float64) float64 {
	prev := tf.Get(chunkHash)
	limited := ApplyRateLimit(prev, rawTemp)
	tf.Set(chunkHash, limited)
	return limited
}

// Decay 非订阅 chunk 温度衰减 + 写回
func (tf *TempField) Decay(chunkHash uint64) float64 {
	prev := tf.Get(chunkHash)
	decayed := ApplyDecay(prev)
	tf.Set(chunkHash, decayed)
	return decayed
}
