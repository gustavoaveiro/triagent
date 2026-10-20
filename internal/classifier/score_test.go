package classifier

import "testing"

func TestGpuProviderNameForGOOS(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{"darwin", "coreml"},
		{"windows", "directml"},
		{"linux", "cuda"},
		{"freebsd", "cuda"}, // anything else defaults to the CUDA attempt
	}
	for _, tc := range tests {
		if got := gpuProviderNameForGOOS(tc.goos); got != tc.want {
			t.Errorf("gpuProviderNameForGOOS(%q) = %q, want %q", tc.goos, got, tc.want)
		}
	}
}

func TestSoftmaxNSFW(t *testing.T) {
	tests := []struct {
		name   string
		logits []float32
		want   float64
		tol    float64
	}{
		{"confidently safe", []float32{4.49, -3.53}, 0.0003, 0.001},
		{"confidently explicit", []float32{-4.49, 3.53}, 0.9997, 0.001},
		{"equal logits means 50/50", []float32{1.0, 1.0}, 0.5, 1e-9},
		{"large magnitude doesn't overflow", []float32{-1000, 1000}, 1.0, 1e-9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := softmaxNSFW(tc.logits)
			if diff := got - tc.want; diff > tc.tol || diff < -tc.tol {
				t.Errorf("softmaxNSFW(%v) = %v, want %v (+/- %v)", tc.logits, got, tc.want, tc.tol)
			}
		})
	}
}
