package hoststats

import (
	"testing"
)

func TestParseNvidiaSMI(t *testing.T) {
	// --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw
	// --format=csv,noheader,nounits  (memory in MiB)
	csv := "0, NVIDIA RTX A2000, 12, 1024, 6138, 45, 18.42\n"
	got := ParseNvidiaSMI(csv)
	if len(got) != 1 {
		t.Fatalf("got %d gpus, want 1", len(got))
	}
	g := got[0]
	if g.Index != 0 || g.Name != "NVIDIA RTX A2000" {
		t.Errorf("index/name = %d/%q", g.Index, g.Name)
	}
	if g.UtilPercent != 12 {
		t.Errorf("util = %v, want 12", g.UtilPercent)
	}
	if g.MemUsedBytes == nil || *g.MemUsedBytes != 1024*1024*1024 { // 1024 MiB
		t.Errorf("memUsed = %v, want %d", g.MemUsedBytes, 1024*1024*1024)
	}
	if g.MemTotalBytes == nil || *g.MemTotalBytes != 6138*1024*1024 {
		t.Errorf("memTotal = %v", g.MemTotalBytes)
	}
	if g.TempC == nil || *g.TempC != 45 {
		t.Errorf("tempC = %v, want 45", g.TempC)
	}
	if g.PowerW == nil || *g.PowerW != 18.42 {
		t.Errorf("powerW = %v, want 18.42", g.PowerW)
	}
}

func TestParseTegrastats(t *testing.T) {
	line := "RAM 4096/30536MB (lfb 5x4MB) SWAP 0/15268MB (cached 0MB) " +
		"CPU [10%@1190,5%@1190] GR3D_FREQ 37% cpu@49C GPU@48C " +
		"VDD_GPU_SOC 1234mW/1234mW"
	got := ParseTegrastats(line)
	if len(got) != 1 {
		t.Fatalf("got %d gpus, want 1", len(got))
	}
	g := got[0]
	if g.UtilPercent != 37 {
		t.Errorf("util = %v, want 37", g.UtilPercent)
	}
	if g.TempC == nil || *g.TempC != 48 {
		t.Errorf("tempC = %v, want 48", g.TempC)
	}
	if g.PowerW == nil || *g.PowerW != 1.234 { // 1234 mW
		t.Errorf("powerW = %v, want 1.234", g.PowerW)
	}
	if g.MemUsedBytes != nil || g.MemTotalBytes != nil {
		t.Errorf("mem = %v/%v, want nil/nil (unified memory has no per-GPU figure)", g.MemUsedBytes, g.MemTotalBytes)
	}
}

func TestParseTegrastatsNoGPUFields(t *testing.T) {
	// A line with no GR3D_FREQ should yield no GPU entries rather than a bogus 0%.
	got := ParseTegrastats("RAM 100/200MB CPU [1%@100]")
	if len(got) != 0 {
		t.Errorf("got %d gpus, want 0", len(got))
	}
}

func TestParseNvidiaSMIUnifiedMemoryNA(t *testing.T) {
	// Jetson (unified memory): nvidia-smi answers "[N/A]" for both memory
	// fields. They must stay nil — "not applicable" is not the same as 0.
	// Observed on a Jetson AGX Thor (JetPack 7.2), 2026-07-02.
	csv := "0, NVIDIA Thor, 85, [N/A], [N/A], 62, 37.53\n"
	got := ParseNvidiaSMI(csv)
	if len(got) != 1 {
		t.Fatalf("got %d gpus, want 1", len(got))
	}
	g := got[0]
	if g.MemUsedBytes != nil {
		t.Errorf("memUsed = %d, want nil for [N/A]", *g.MemUsedBytes)
	}
	if g.MemTotalBytes != nil {
		t.Errorf("memTotal = %d, want nil for [N/A]", *g.MemTotalBytes)
	}
	if g.UtilPercent != 85 {
		t.Errorf("util = %v, want 85", g.UtilPercent)
	}
	if g.TempC == nil || *g.TempC != 62 {
		t.Errorf("tempC = %v, want 62", g.TempC)
	}
	if g.PowerW == nil || *g.PowerW != 37.53 {
		t.Errorf("powerW = %v, want 37.53", g.PowerW)
	}
}
