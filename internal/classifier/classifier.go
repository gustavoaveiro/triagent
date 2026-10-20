package classifier

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	inputName  = "pixel_values"
	outputName = "logits"
)

// Options configures a new Classifier.
type Options struct {
	// ModelPath is the .onnx model file (see AdamCodd/vit-base-nsfw-detector).
	ModelPath string
	// LibraryPath is the onnxruntime shared library (libonnxruntime.so /
	// .dylib / .dll) for the current platform.
	LibraryPath string
	// PreferGPU attempts a platform-appropriate GPU execution provider
	// (CUDA on Linux/Windows, CoreML on macOS) before falling back to CPU.
	PreferGPU bool
}

// Classifier scores images for explicit/nsfw content using a loaded ONNX
// model. A single Classifier is safe for concurrent use: Score serializes
// access internally, since inference - not I/O - is the actual bottleneck
// regardless of caller parallelism.
type Classifier struct {
	mu       sync.Mutex
	session  *ort.AdvancedSession
	input    *ort.Tensor[float32]
	output   *ort.Tensor[float32]
	provider string
}

// New loads the ONNX model at opts.ModelPath using the onnxruntime shared
// library at opts.LibraryPath. Call Close when done.
func New(opts Options) (*Classifier, error) {
	ort.SetSharedLibraryPath(opts.LibraryPath)
	if !ort.IsInitialized() {
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("initialize onnxruntime: %w", err)
		}
	}

	input, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, InputSize, InputSize))
	if err != nil {
		return nil, fmt.Errorf("create input tensor: %w", err)
	}
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 2))
	if err != nil {
		input.Destroy()
		return nil, fmt.Errorf("create output tensor: %w", err)
	}

	session, provider, err := newSession(opts, input, output)
	if err != nil {
		input.Destroy()
		output.Destroy()
		return nil, err
	}

	return &Classifier{session: session, input: input, output: output, provider: provider}, nil
}

// Provider reports which execution provider ended up active: "cuda",
// "coreml", "directml" or "cpu".
func (c *Classifier) Provider() string {
	return c.provider
}

// Close releases the session and its tensors. It does not shut down the
// shared onnxruntime environment, since other Classifiers (if any) may
// still depend on it; call Shutdown once at process exit for that.
func (c *Classifier) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	errS := c.session.Destroy()
	errI := c.input.Destroy()
	errO := c.output.Destroy()
	if errS != nil {
		return errS
	}
	if errI != nil {
		return errI
	}
	return errO
}

// Shutdown tears down the process-wide onnxruntime environment. Call it
// once, after all Classifiers have been closed.
func Shutdown() error {
	if !ort.IsInitialized() {
		return nil
	}
	return ort.DestroyEnvironment()
}

// Score returns the model's estimated probability (0-1) that the image at
// path contains explicit content.
func (c *Classifier) Score(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open image: %w", err)
	}
	defer f.Close()
	return c.ScoreReader(f)
}

// ScoreReader is like Score, but reads image data from an already-open
// reader (used by the video package to score extracted frames without an
// extra path round-trip, and by tests).
func (c *Classifier) ScoreReader(r io.Reader) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := preprocess(r, c.input.GetData()); err != nil {
		return 0, fmt.Errorf("decode image: %w", err)
	}
	if err := c.session.Run(); err != nil {
		return 0, fmt.Errorf("run inference: %w", err)
	}
	return softmaxNSFW(c.output.GetData()), nil
}

func newSession(opts Options, input, output *ort.Tensor[float32]) (*ort.AdvancedSession, string, error) {
	if opts.PreferGPU {
		if session, provider, err := tryGPUSession(opts.ModelPath, input, output); err == nil {
			return session, provider, nil
		}
	}
	session, err := ort.NewAdvancedSession(opts.ModelPath,
		[]string{inputName}, []string{outputName},
		[]ort.Value{input}, []ort.Value{output}, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create cpu session: %w", err)
	}
	return session, "cpu", nil
}

func tryGPUSession(modelPath string, input, output *ort.Tensor[float32]) (*ort.AdvancedSession, string, error) {
	sessionOptions, err := ort.NewSessionOptions()
	if err != nil {
		return nil, "", err
	}
	defer sessionOptions.Destroy()

	provider, err := appendGPUProvider(sessionOptions)
	if err != nil {
		return nil, "", err
	}

	session, err := ort.NewAdvancedSession(modelPath,
		[]string{inputName}, []string{outputName},
		[]ort.Value{input}, []ort.Value{output}, sessionOptions)
	if err != nil {
		return nil, "", err
	}
	return session, provider, nil
}

// gpuProviderNameForGOOS is the pure policy half of GPU provider
// selection - which provider to attempt for a given OS - kept separate
// from appendGPUProvider's actual cgo calls so it's unit-testable without
// needing matching GPU hardware for every platform.
func gpuProviderNameForGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "coreml"
	case "windows":
		return "directml"
	default:
		return "cuda"
	}
}

func appendGPUProvider(opts *ort.SessionOptions) (string, error) {
	provider := gpuProviderNameForGOOS(runtime.GOOS)
	switch provider {
	case "coreml":
		if err := opts.AppendExecutionProviderCoreML(0); err != nil {
			return "", err
		}
	case "directml":
		if err := opts.AppendExecutionProviderDirectML(0); err != nil {
			return "", err
		}
	default: // cuda
		cudaOpts, err := ort.NewCUDAProviderOptions()
		if err != nil {
			return "", err
		}
		defer cudaOpts.Destroy()
		if err := cudaOpts.Update(map[string]string{"device_id": "0"}); err != nil {
			return "", err
		}
		if err := opts.AppendExecutionProviderCUDA(cudaOpts); err != nil {
			return "", err
		}
	}
	return provider, nil
}
