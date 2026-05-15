package auth

import "time"

func nowUnixMilli() int64 {
	return time.Now().UnixMilli()
}
