// 权重计算。README §11.4 第一版公式:
//   Weight = (Urgency * Impact * Consistency * RegionFactor) / (Load * NetworkCost) + BatchGain - SplitPenalty
//   RegionFactor = 1.0 + 0.3 * (RegionCount - 1)
package weight

import (
	"github.com/symc/sim/pkg/cell"
	"github.com/symc/sim/pkg/layer"
)

type Context struct {
	Urgency, Impact, Consistency, Load, NetworkCost float64
	BatchGain, SplitPenalty                         float64
	RegionCount                                     int
}

func Compute(c cell.Cell, l layer.Layer, ctx Context) float64 {
	_ = c
	_ = l
	if ctx.RegionCount < 1 {
		ctx.RegionCount = 1
	}
	regionFactor := 1.0 + 0.3*float64(ctx.RegionCount-1)
	num := ctx.Urgency * ctx.Impact * ctx.Consistency * regionFactor
	den := ctx.Load * ctx.NetworkCost
	if den == 0 {
		den = 0.0001 // ponytail: 防爆,真实负载理论上 > 0
	}
	return num/den + ctx.BatchGain - ctx.SplitPenalty
}