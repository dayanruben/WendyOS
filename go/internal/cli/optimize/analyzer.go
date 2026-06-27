package optimize

// Analyzer inspects a Target and returns findings.
type Analyzer interface {
	ID() string
	Analyze(t *Target) []Finding
}

// DefaultAnalyzers returns all built-in analyzers. Each analyzer task appends here.
func DefaultAnalyzers() []Analyzer {
	return []Analyzer{
		// appended by later tasks: buildCacheAnalyzer{}, releaseDebugAnalyzer{}, cudaMLAnalyzer{}, archImageAnalyzer{}
	}
}

// Analyze runs every analyzer over every target, in target-then-analyzer order.
func Analyze(targets []Target, analyzers []Analyzer) []Finding {
	var out []Finding
	for i := range targets {
		t := &targets[i]
		for _, a := range analyzers {
			out = append(out, a.Analyze(t)...)
		}
	}
	return out
}
