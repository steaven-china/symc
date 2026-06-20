// symc sim 入口。
// MVP 阶段 1:standalone cell scheduler 模拟器骨架,先跑通再填逻辑。
package main

import (
	"fmt"
	"os"

	"github.com/symc/sim/pkg/cell"
	"github.com/symc/sim/pkg/layer"
	"github.com/symc/sim/pkg/weight"
)

func main() {
	c := cell.New(0, 0, 0, 50_000_000) // chunk(0,0), tick 0, 50ms
	l := layer.Classify(c)
	w := weight.Compute(c, l, weight.Context{
		Urgency: 1.0, Impact: 1.0, Consistency: 1.0,
		Load: 0.5, NetworkCost: 0.5,
	})
	fmt.Fprintf(os.Stdout,
		"symc sim: cell=%s layer=%s weight=%.2f\n",
		c, l, w,
	)
}