package mathanalysis

import (
	"math"
	"math/bits"
)

// dftPowers preserves the original N-point frequency grid, omitting DC and retaining
// Nyquist for even N. Bluestein reduces non-power-of-two lengths to a convolution;
// padding the input itself would change the grid and the peak/background statistic.
func dftPowers(points []float64) []float64 {
	n := len(points)
	if n < 2 {
		return nil
	}
	powers := make([]float64, n/2)
	if n&(n-1) == 0 {
		spectrum := make([]complex128, n)
		for i, x := range points {
			spectrum[i] = complex(x, 0)
		}
		radix2FFT(spectrum, false)
		for k := range powers {
			z := spectrum[k+1]
			powers[k] = real(z)*real(z) + imag(z)*imag(z)
		}
		return powers
	}
	m := 1 << bits.Len(uint(2*n-2))
	a, b := make([]complex128, m), make([]complex128, m)
	for j, x := range points {
		// Reduce j² modulo 2N before evaluating the phase to avoid large-angle loss.
		phase := math.Pi * float64((uint64(j)*uint64(j))%uint64(2*n)) / float64(n)
		sin, cos := math.Sincos(phase)
		a[j] = complex(x*cos, -x*sin)
		b[j] = complex(cos, sin)
		if j > 0 {
			b[m-j] = b[j]
		}
	}
	radix2FFT(a, false)
	radix2FFT(b, false)
	for i := range a {
		a[i] *= b[i]
	}
	radix2FFT(a, true)
	// The final chirp has unit magnitude, so it does not change spectral power.
	for k := range powers {
		z := a[k+1]
		powers[k] = real(z)*real(z) + imag(z)*imag(z)
	}
	return powers
}

// In-place Cooley-Tukey, radix two. The inverse divides by N exactly once.
func radix2FFT(values []complex128, inverse bool) {
	n := len(values)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			values[i], values[j] = values[j], values[i]
		}
	}
	for width := 2; width <= n; width <<= 1 {
		angle := -2 * math.Pi / float64(width)
		if inverse {
			angle = -angle
		}
		sin, cos := math.Sincos(angle)
		step := complex(cos, sin)
		half := width >> 1
		for start := 0; start < n; start += width {
			rotation := complex(1, 0)
			for j := 0; j < half; j++ {
				even := values[start+j]
				odd := values[start+j+half] * rotation
				values[start+j] = even + odd
				values[start+j+half] = even - odd
				rotation *= step
			}
		}
	}
	if inverse {
		scale := complex(float64(n), 0)
		for i := range values {
			values[i] /= scale
		}
	}
}
