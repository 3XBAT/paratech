package audiocapture

import (
	"encoding/binary"
	"math"
)

// Windowed-sinc resampler parameters: zeroCrossings per side of the kernel, the pass band as a fraction of the lower
// Nyquist frequency, and the most precomputed phases (more phases are interpolated linearly between rows).
const (
	zeroCrossings = 16
	passBand      = 0.9
	maxPhases     = 1024
	trimAt        = 1 << 14 // drop consumed input once this many samples are behind
)

// resampler converts a mono stream from one rate to another with a Blackman-windowed sinc low-pass filter. Output
// sample k sits at input position k*m/l (l/m = out/in rate reduced), so the timeline is exact for any rate pair.
// Input before the stream and after its end counts as silence.
type resampler struct {
	pass bool // equal rates: samples are copied

	l, m   int64 // output k is at input position k*m/l
	half   int   // half the kernel length in input samples
	phases int64
	table  []float32 // (phases+1) rows of 2*half taps

	buf  []float32 // input samples; buf[0] has absolute index base
	base int64
	k    int64 // next output index
	row  []float32
}

func newResampler(inRate, outRate int) *resampler {
	if inRate == outRate {
		return &resampler{pass: true}
	}
	g := gcd(int64(inRate), int64(outRate))
	r := &resampler{l: int64(outRate) / g, m: int64(inRate) / g}

	fc := passBand * math.Min(1, float64(outRate)/float64(inRate)) // cut-off as a fraction of the input Nyquist
	r.half = int(math.Ceil(zeroCrossings / fc))
	r.phases = min(r.l, maxPhases)

	taps := 2 * r.half
	r.table = make([]float32, int(r.phases+1)*taps)
	for p := int64(0); p <= r.phases; p++ {
		frac := float64(p) / float64(r.phases)
		row := r.table[int(p)*taps : int(p+1)*taps]
		var sum float64
		coef := make([]float64, taps)
		for i := range taps {
			t := frac - float64(i-r.half+1) // distance from the output position to input sample n0+j
			coef[i] = fc * sinc(fc*t) * blackman(t/float64(r.half))
			sum += coef[i]
		}
		for i := range taps {
			row[i] = float32(coef[i] / sum) // unity gain at DC in every phase
		}
	}

	// The samples before the stream are silence.
	r.buf = make([]float32, r.half)
	r.base = -int64(r.half)
	r.row = make([]float32, taps)
	return r
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

// blackman is the Blackman window on [-1, 1].
func blackman(x float64) float64 {
	if x <= -1 || x >= 1 {
		return 0
	}
	return 0.42 + 0.5*math.Cos(math.Pi*x) + 0.08*math.Cos(2*math.Pi*x)
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// push adds input samples and appends every output whose kernel is covered to out.
func (r *resampler) push(in []float32, out []float32) []float32 {
	if r.pass {
		return append(out, in...)
	}
	r.buf = append(r.buf, in...)
	return r.drain(out, -1)
}

// flush appends exactly n more outputs, treating the input after the end of the stream as silence.
func (r *resampler) flush(n int64, out []float32) []float32 {
	if n <= 0 {
		return out
	}
	if r.pass {
		for range n {
			out = append(out, 0)
		}
		return out
	}
	last := r.k + n - 1
	need := (last*r.m)/r.l + int64(r.half) + 1 - (r.base + int64(len(r.buf)))
	for range max(need, 0) {
		r.buf = append(r.buf, 0)
	}
	return r.drain(out, n)
}

// drain computes outputs while their input is buffered, at most limit of them (limit < 0: no limit).
func (r *resampler) drain(out []float32, limit int64) []float32 {
	end := r.base + int64(len(r.buf)) // first index not buffered
	taps := 2 * r.half
	for ; limit != 0; limit-- {
		pos := r.k * r.m
		n0, frac := pos/r.l, pos%r.l
		if n0+int64(r.half) >= end {
			break
		}

		coef := r.coefficients(frac)
		start := int(n0 - int64(r.half) + 1 - r.base)
		var acc float32
		for i, x := range r.buf[start : start+taps] {
			acc += x * coef[i]
		}
		out = append(out, acc)
		r.k++
	}

	// Drop the input no later output needs.
	if keep := (r.k*r.m)/r.l - int64(r.half) + 1; keep-r.base > trimAt {
		drop := int(keep - r.base)
		r.buf = r.buf[:copy(r.buf, r.buf[drop:])]
		r.base = keep
	}
	return out
}

// coefficients returns the kernel row for an output at fractional position frac/l between two input samples.
func (r *resampler) coefficients(frac int64) []float32 {
	taps := 2 * r.half
	x := frac * r.phases
	i, rem := x/r.l, x%r.l
	lo := r.table[int(i)*taps : int(i+1)*taps]
	if rem == 0 {
		return lo
	}
	hi := r.table[int(i+1)*taps : int(i+2)*taps]
	a := float32(rem) / float32(r.l)
	for j := range taps {
		r.row[j] = lo[j] + a*(hi[j]-lo[j])
	}
	return r.row
}

// outputFrames is the number of output frames for n input frames: the timeline rounded to the nearest sample.
func outputFrames(n int64, inRate, outRate uint32) int64 {
	if inRate == outRate {
		return n
	}
	return (n*int64(outRate) + int64(inRate)/2) / int64(inRate)
}

// toMono decodes interleaved frames of f and appends their channel average to out.
func toMono(f Format, data []byte, out []float32) []float32 {
	ch := int(f.Channels)
	width := int(f.BitsPerSample) / 8
	block := ch * width
	le := binary.LittleEndian
	inv := 1 / float32(ch)

	for off := 0; off+block <= len(data); off += block {
		var sum float32
		for c := range ch {
			s := data[off+c*width : off+(c+1)*width]
			switch {
			case f.Float:
				v := math.Float32frombits(le.Uint32(s))
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) { // from a broken source
					v = 0
				}
				sum += v
			case width == 2:
				sum += float32(int16(le.Uint16(s))) / (1 << 15) //nolint:gosec // reinterpreting the sign bit
			case width == 3:
				v := int32(uint32(s[0])<<8|uint32(s[1])<<16|uint32(s[2])<<24) >> 8 //nolint:gosec // sign-extends 24 bits
				sum += float32(v) / (1 << 23)
			default:
				sum += float32(int32(le.Uint32(s))) / (1 << 31) //nolint:gosec // reinterpreting the sign bit
			}
		}
		out = append(out, sum*inv)
	}
	return out
}

// appendPCM16 encodes samples as 16-bit PCM, clipping what is out of range.
func appendPCM16(dst []byte, samples []float32) []byte {
	for _, v := range samples {
		x := math.Round(float64(v) * math.MaxInt16)
		x = math.Max(math.MinInt16, math.Min(math.MaxInt16, x))
		dst = binary.LittleEndian.AppendUint16(dst, uint16(int16(x))) //nolint:gosec // clipped to the int16 range
	}
	return dst
}
