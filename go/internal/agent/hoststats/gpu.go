package hoststats

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GPUStat is a single GPU's instantaneous utilization snapshot.
type GPUStat struct {
	Index         uint32
	Name          string
	UtilPercent   float64
	MemUsedBytes  int64
	MemTotalBytes int64
	TempC         *float64
	PowerW        *float64
}

// ParseNvidiaSMI parses CSV output of:
//   nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw
//              --format=csv,noheader,nounits
// Memory fields are MiB. Missing/[N/A] numeric fields are treated as zero/nil.
func ParseNvidiaSMI(csv string) []GPUStat {
	var out []GPUStat
	for _, line := range strings.Split(csv, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Split(line, ",")
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		if len(f) < 5 {
			continue
		}
		g := GPUStat{Name: f[1]}
		if v, err := strconv.ParseUint(f[0], 10, 32); err == nil {
			g.Index = uint32(v)
		}
		if v, err := strconv.ParseFloat(f[2], 64); err == nil {
			g.UtilPercent = v
		}
		if v, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			g.MemUsedBytes = v * 1024 * 1024
		}
		if v, err := strconv.ParseInt(f[4], 10, 64); err == nil {
			g.MemTotalBytes = v * 1024 * 1024
		}
		if len(f) > 5 {
			if v, err := strconv.ParseFloat(f[5], 64); err == nil {
				g.TempC = &v
			}
		}
		if len(f) > 6 {
			if v, err := strconv.ParseFloat(f[6], 64); err == nil {
				g.PowerW = &v
			}
		}
		out = append(out, g)
	}
	return out
}

var (
	tegraGR3DRe  = regexp.MustCompile(`GR3D_FREQ (\d+)%`)
	tegraGPUTemp = regexp.MustCompile(`GPU@([\d.]+)C`)
	tegraGPUPwr  = regexp.MustCompile(`VDD_GPU_SOC (\d+)mW`)
)

// ParseTegrastats extracts the integrated-GPU utilization (GR3D_FREQ) and, when
// present, GPU temperature and power from a single tegrastats line. Jetson uses
// unified memory, so per-GPU memory is left zero. Returns no entries when the
// line has no GR3D_FREQ field.
func ParseTegrastats(line string) []GPUStat {
	m := tegraGR3DRe.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	g := GPUStat{Name: "Integrated GPU"}
	if v, err := strconv.ParseFloat(m[1], 64); err == nil {
		g.UtilPercent = v
	}
	if t := tegraGPUTemp.FindStringSubmatch(line); t != nil {
		if v, err := strconv.ParseFloat(t[1], 64); err == nil {
			g.TempC = &v
		}
	}
	if p := tegraGPUPwr.FindStringSubmatch(line); p != nil {
		if mw, err := strconv.ParseFloat(p[1], 64); err == nil {
			w := mw / 1000.0
			g.PowerW = &w
		}
	}
	return []GPUStat{g}
}

// SampleGPU returns a one-shot GPU sample, preferring nvidia-smi (discrete GPUs)
// and falling back to tegrastats (Jetson). Returns nil when neither tool is
// available — callers treat that as "no GPU panel", not an error.
func SampleGPU(ctx context.Context) []GPUStat {
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cctx, path,
			"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
			"--format=csv,noheader,nounits").Output()
		if err == nil {
			if gpus := ParseNvidiaSMI(string(out)); len(gpus) > 0 {
				return gpus
			}
		}
	}
	if path, err := exec.LookPath("tegrastats"); err == nil {
		// tegrastats streams; --interval + a short deadline yields one line.
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(cctx, path, "--interval", "500", "--count", "1").Output()
		if line := strings.TrimSpace(string(out)); line != "" {
			return ParseTegrastats(line)
		}
	}
	return nil
}
