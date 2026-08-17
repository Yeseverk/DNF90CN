package protocol

import "fmt"

func formatUnix(t interface{ Unix() int64 }) string {
	return fmt.Sprintf("%d", t.Unix())
}
