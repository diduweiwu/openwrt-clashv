package core

import "os"

// rotateLog 超过 max 字节时把当前日志挪到 .old（仅保留一份旧档）。
// 在每次打开日志文件前调用，避免长期运行把路由器 flash 写满。
func rotateLog(path string, max int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < max {
		return
	}
	_ = os.Remove(path + ".old")
	_ = os.Rename(path, path+".old")
}
