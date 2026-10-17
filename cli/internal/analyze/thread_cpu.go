package analyze

// CPU is an average between non-atomic scans, with the kernel tick's quantization.
// CorePercent is one logical core, not a share of the device's total CPU capacity.
type ThreadCPUDelta struct {
	ElapsedMS, UserTicks, SystemTicks uint64
	CPUTimeMS, CorePercent, TickMS    float64
}

func threadCPUDelta(before, after ThreadHistoryPoint) *ThreadCPUDelta {
	if before.Row.TID != after.Row.TID || before.Row.StartTicks != after.Row.StartTicks || after.ElapsedMS <= before.ElapsedMS ||
		before.TicksPerSecond == 0 || before.TicksPerSecond != after.TicksPerSecond ||
		after.Row.UserTicks < before.Row.UserTicks || after.Row.SystemTicks < before.Row.SystemTicks {
		return nil
	}
	elapsed := after.ElapsedMS - before.ElapsedMS
	user, system := after.Row.UserTicks-before.Row.UserTicks, after.Row.SystemTicks-before.Row.SystemTicks
	// Convert before summing: valid counters must not wrap a uint64 sum.
	tick := 1000 / float64(after.TicksPerSecond)
	cpu := (float64(user) + float64(system)) * tick
	return &ThreadCPUDelta{ElapsedMS: elapsed, UserTicks: user, SystemTicks: system, CPUTimeMS: cpu, CorePercent: cpu / float64(elapsed) * 100, TickMS: tick}
}
