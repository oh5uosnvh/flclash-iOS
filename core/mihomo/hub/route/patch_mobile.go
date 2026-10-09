//go:build android || ios

package route

func init() {
	SetEmbedMode(true)
}
