package classifier

import "math"

// softmaxNSFW converts the model's two raw logits (index 0 = sfw, index 1
// = nsfw, per its published config) into the probability that the image
// is explicit content.
func softmaxNSFW(logits []float32) float64 {
	m := math.Max(float64(logits[0]), float64(logits[1]))
	e0 := math.Exp(float64(logits[0]) - m)
	e1 := math.Exp(float64(logits[1]) - m)
	return e1 / (e0 + e1)
}
