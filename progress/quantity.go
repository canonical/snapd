package progress

import (
	"fmt"
	"math"
)

// these are taken from github.com/chipaca/quantity with permission :-)

func formatAmount(amount uint64, width int) string {
	if width < 0 {
		width = 5
	}
	max := uint64(5000)
	maxFloat := 999.5

	if width < 4 {
		width = 3
		max = 999
		maxFloat = 99.5
	}

	if amount <= max {
		pad := ""
		if width > 5 {
			pad = " "
		}
		return fmt.Sprintf("%*d%s", width-len(pad), amount, pad)
	}
	var prefix rune
	r := float64(amount)
	// zetta and yotta are me being pedantic: maxuint64 is ~18EB
	for _, prefix = range "kMGTPEZY" {
		r /= 1000
		if r < maxFloat {
			break
		}
	}

	width--
	digits := 3
	if r < 99.5 {
		digits--
		if r < 9.5 {
			digits--
			if r < .95 {
				digits--
			}
		}
	}
	precision := 0
	if (width - digits) > 1 {
		precision = width - digits - 1
	}

	s := fmt.Sprintf("%*.*f%c", width, precision, r, prefix)
	if r < .95 {
		return s[1:]
	}
	return s
}

func formatBPS(n, sec float64, width int) string {
	if sec < 0 {
		sec = -sec
	}
	return formatAmount(uint64(n/sec), width-2) + "B/s"
}

const (
	period = 365.25 // julian years (c.f. the actual orbital period, 365.256363004d)
)

func divmod(a, b float64) (q, r float64) {
	q = math.Floor(a / b)
	return q, a - q*b
}

func formatDuration(dt float64) string {
	if dt < 60 {
		if dt >= 9.995 {
			return fmt.Sprintf("%.1fs", dt)
		} else if dt >= .9995 {
			return fmt.Sprintf("%.2fs", dt)
		}

		var prefix rune
		for _, prefix = range "mun" {
			dt *= 1000
			if dt >= .9995 {
				break
			}
		}

		if dt > 9.5 {
			return fmt.Sprintf("%3.f%cs", dt, prefix)
		}

		return fmt.Sprintf("%.1f%cs", dt, prefix)
	}

	if dt < 600 {
		m, s := divmod(dt, 60)
		return fmt.Sprintf("%.fm%02.fs", m, s)
	}

	dt /= 60 // dt now minutes

	if dt < 99.95 {
		return fmt.Sprintf("%3.1fm", dt)
	}

	if dt < 10*60 {
		h, m := divmod(dt, 60)
		return fmt.Sprintf("%.fh%02.fm", h, m)
	}

	if dt < 24*60 {
		if h, m := divmod(dt, 60); m < 10 {
			return fmt.Sprintf("%.fh%1.fm", h, m)
		}

		return fmt.Sprintf("%3.1fh", dt/60)
	}

	dt /= 60 // dt now hours

	if dt < 10*24 {
		d, h := divmod(dt, 24)
		return fmt.Sprintf("%.fd%02.fh", d, h)
	}

	if dt < 99.95*24 {
		if d, h := divmod(dt, 24); h < 10 {
			return fmt.Sprintf("%.fd%.fh", d, h)
		}
		return fmt.Sprintf("%4.1fd", dt/24)
	}

	dt /= 24 // dt now days

	if dt < 2*period {
		return fmt.Sprintf("%4.0fd", dt)
	}

	dt /= period // dt now years

	if dt < 9.995 {
		return fmt.Sprintf("%4.2fy", dt)
	}

	if dt < 99.95 {
		return fmt.Sprintf("%4.1fy", dt)
	}

	return fmt.Sprintf("%4.fy", dt)
}
