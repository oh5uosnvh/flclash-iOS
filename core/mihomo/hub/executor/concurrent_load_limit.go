//go:build (!386 && !amd64 && !arm64 && !arm64be && !mipsle && !mips) || (with_low_memory && !mips && !mipsle)

package executor

const concurrentCount = 5
