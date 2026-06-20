// 四层事件分级。MVP 阶段先按 cell 的 tick 频率粗略分,后面挂真实事件源。
package layer

import "github.com/symc/sim/pkg/cell"

type Layer int

const (
	Static     Layer = iota // 地形、光照
	SemiStatic              // 休眠红石/漏斗
	Dynamic                 // 玩家、战斗、TNT
	Ephemeral               // 粒子、声音
)

func (l Layer) String() string {
	switch l {
	case Static:
		return "static"
	case SemiStatic:
		return "semi-static"
	case Dynamic:
		return "dynamic"
	case Ephemeral:
		return "ephemeral"
	}
	return "unknown"
}

// ponytail: 先按 tick % 20 粗分,只是占位骨架;真实实现看 README §2.2 的触发条件。
func Classify(c cell.Cell) Layer {
	switch {
	case c.Tick%20 == 0:
		return Static
	case c.Tick%5 == 0:
		return SemiStatic
	case c.WindowNS < 60_000_000:
		return Dynamic
	default:
		return Ephemeral
	}
}