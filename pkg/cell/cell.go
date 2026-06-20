// 原子处理单元: (chunk_x, chunk_z, tick, time_window_ns)。
// 同一个 cell 内部必须一起处理,不能拆。
package cell

import "fmt"

type Cell struct {
	CX, CZ    int
	Tick      int64
	WindowNS  int64 // time_window in nanoseconds
}

func New(cx, cz int, tick int64, windowNS int64) Cell {
	return Cell{CX: cx, CZ: cz, Tick: tick, WindowNS: windowNS}
}

func (c Cell) String() string {
	return fmt.Sprintf("(chunk=%d,%d tick=%d win=%dns)", c.CX, c.CZ, c.Tick, c.WindowNS)
}