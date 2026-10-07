package tag

import (
	"fmt"
	"strconv"
)

type mp4Integer struct {
	atom    string
	width   int
	maximum uint64
}

var mp4Integers = map[string]mp4Integer{
	"bpm": {"tmpo", 2, 65535}, "compilation": {"cpil", 1, 1}, "gapless": {"pgap", 1, 1},
	"mediakind": {"stik", 1, 255}, "advisory": {"rtng", 1, 255}, "podcast": {"pcst", 1, 1},
	"tvseason": {"tvsn", 4, 0xffffffff}, "tvepisode": {"tves", 4, 0xffffffff},
}

func (v *mp4) setInteger(k string, values []string) (bool, error) {
	spec, ok := mp4Integers[k]
	if !ok {
		return false, nil
	}
	if len(values) != 1 {
		return true, fmt.Errorf("%s requires one integer", k)
	}
	text := values[0]
	if spec.maximum == 1 {
		if text == "true" {
			text = "1"
		}
		if text == "false" {
			text = "0"
		}
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil || n > spec.maximum {
		return true, fmt.Errorf("%s must be in 0..%d", k, spec.maximum)
	}
	data := make([]byte, spec.width)
	for i := len(data) - 1; i >= 0; i-- {
		data[i] = byte(n)
		n >>= 8
	}
	i := v.ilst(true)
	i.remove(spec.atom)
	i.children = append(i.children, &atom{typ: spec.atom, data: dataAtom(21, data).encode()})
	return true, nil
}
