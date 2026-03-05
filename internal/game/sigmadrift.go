package game

import (
	"math"
	"math/rand"
)

// TrajectoryPoint represents a single point in a mouse movement trajectory.
type TrajectoryPoint struct {
	X, Y float64
	T    float64 // time in milliseconds
}

// SigmaDriftConfig holds tunable parameters for the SigmaDrift mouse movement algorithm.
// Based on Plamondon's Kinematic Theory of rapid human movements.
type SigmaDriftConfig struct {
	// Fitts' law parameters controlling movement time.
	FittsA      float64
	FittsB      float64
	TargetWidth float64

	// Primary stroke undershooting/overshooting.
	UndershootMin   float64
	UndershootMax   float64
	PeakTimeRatio   float64
	PrimarySigmaMin float64
	PrimarySigmaMax float64

	// Overshoot and correction sub-movements.
	OvershootProb        float64
	OvershootMin         float64
	OvershootMax         float64
	CorrectionSigmaMin   float64
	CorrectionSigmaMax   float64
	SecondCorrectionProb float64

	// Lateral arc curvature.
	CurvatureScale float64

	// Ornstein-Uhlenbeck hand drift.
	OUTheta float64
	OUSigma float64

	// Physiological tremor (8-12 Hz).
	TremorFreqMin float64
	TremorFreqMax float64
	TremorAmpMin  float64
	TremorAmpMax  float64

	// Signal-dependent noise (Harris-Wolpert).
	SDNK float64

	// Gamma-distributed inter-sample timing.
	SampleDtMean float64
	GammaShape   float64

	// SpeedMultiplier scales the total movement time.
	// 1.0 = realistic human speed, lower = faster.
	SpeedMultiplier float64
}

// DefaultSigmaDriftConfig returns a SigmaDrift configuration tuned for bot use.
// Movements are approximately half the duration of realistic human movements.
func DefaultSigmaDriftConfig() SigmaDriftConfig {
	return SigmaDriftConfig{
		FittsA:      25.0,
		FittsB:      80.0,
		TargetWidth: 20.0,

		UndershootMin:   0.92,
		UndershootMax:   0.97,
		PeakTimeRatio:   0.35,
		PrimarySigmaMin: 0.18,
		PrimarySigmaMax: 0.28,

		OvershootProb:        0.15,
		OvershootMin:         1.02,
		OvershootMax:         1.08,
		CorrectionSigmaMin:   0.12,
		CorrectionSigmaMax:   0.20,
		SecondCorrectionProb: 0.25,

		CurvatureScale: 0.025,
		OUTheta:        3.5,
		OUSigma:        1.2,

		TremorFreqMin: 8.0,
		TremorFreqMax: 12.0,
		TremorAmpMin:  0.15,
		TremorAmpMax:  0.55,

		SDNK: 0.04,

		SampleDtMean: 16.0,
		GammaShape:   3.5,

		SpeedMultiplier: 0.5,
	}
}

// sdNormalCDF computes the standard normal cumulative distribution function.
func sdNormalCDF(x float64) float64 {
	return 0.5 * (1.0 + math.Erf(x/math.Sqrt2))
}

// sdLognormalCDF computes the CDF of a lognormal distribution with offset t0.
func sdLognormalCDF(t, t0, mu, sigma float64) float64 {
	if t <= t0 {
		return 0.0
	}
	return sdNormalCDF((math.Log(t-t0) - mu) / sigma)
}

// sdLognormalPDF computes the PDF of a lognormal distribution with offset t0.
func sdLognormalPDF(t, t0, mu, sigma float64) float64 {
	if t <= t0 {
		return 0.0
	}
	dt := t - t0
	z := (math.Log(dt) - mu) / sigma
	return 1.0 / (sigma * math.Sqrt(2.0*math.Pi) * dt) * math.Exp(-0.5*z*z)
}

// sdCurvatureProfile returns s^2*(1-s)^3 normalized to peak=1 at s=0.4.
// Curvature is maximal during the acceleration phase.
func sdCurvatureProfile(s float64) float64 {
	if s <= 0.0 || s >= 1.0 {
		return 0.0
	}
	v := s * s * (1.0 - s) * (1.0 - s) * (1.0 - s)
	const norm = 0.4 * 0.4 * 0.6 * 0.6 * 0.6
	return v / norm
}

// sdDirectionFactor adjusts curvature amplitude based on movement direction.
// Vertical movements produce more curvature due to wrist/forearm geometry.
func sdDirectionFactor(angle float64) float64 {
	sa := math.Abs(math.Sin(angle))
	ca := math.Abs(math.Cos(angle))
	return 0.5 + 0.8*sa - 0.15*ca
}

type sdCorrection struct {
	D     float64
	t0    float64
	mu    float64
	sigma float64
	dirX  float64
	dirY  float64
}

// GenerateTrajectory creates a human-like mouse trajectory from (x0,y0) to (x1,y1)
// using the SigmaDrift algorithm (sigma-lognormal velocity primitives with biomechanical noise).
func GenerateTrajectory(x0, y0, x1, y1 float64, cfg SigmaDriftConfig) []TrajectoryPoint {
	rng := rand.New(rand.NewSource(rand.Int63()))

	uniform := func(lo, hi float64) float64 {
		return lo + rng.Float64()*(hi-lo)
	}
	normal := func(m, s float64) float64 {
		return rng.NormFloat64()*s + m
	}
	gammaDist := func(shape, scale float64) float64 {
		return sdGammaVariate(rng, shape) * scale
	}

	dx := x1 - x0
	dy := y1 - y0
	distance := math.Hypot(dx, dy)
	direction := math.Atan2(dy, dx)

	// Trivial movement — just snap to destination.
	if distance < 1.0 {
		return []TrajectoryPoint{{X: x0, Y: y0, T: 0.0}, {X: x1, Y: y1, T: 50.0}}
	}

	// Unit tangent and normal vectors.
	tx := dx / distance
	ty := dy / distance
	nx := -ty
	ny := tx

	// Fitts' law: index of difficulty → movement time.
	id := math.Log2(distance/cfg.TargetWidth + 1.0)
	mt := (cfg.FittsA + cfg.FittsB*id) * math.Exp(normal(0.0, 0.08))
	if mt < 80.0 {
		mt = 80.0
	}

	// Scale movement time by speed multiplier.
	if cfg.SpeedMultiplier > 0 {
		mt *= cfg.SpeedMultiplier
	}

	// ── Primary stroke ──────────────────────────────────────────
	overshoot := uniform(0.0, 1.0) < cfg.OvershootProb
	var reach float64
	if overshoot {
		reach = uniform(cfg.OvershootMin, cfg.OvershootMax)
	} else {
		reach = uniform(cfg.UndershootMin, cfg.UndershootMax)
	}

	primaryD := distance * reach
	primarySigma := uniform(cfg.PrimarySigmaMin, cfg.PrimarySigmaMax)

	// mu derived so peak velocity lands at peak_t (mode = exp(mu - sigma^2)).
	peakT := mt * uniform(cfg.PeakTimeRatio-0.03, cfg.PeakTimeRatio+0.03)
	primaryMu := math.Log(peakT) + primarySigma*primarySigma

	// ── Correction sub-movements ────────────────────────────────
	var corrections []sdCorrection
	remaining := distance - primaryD
	if math.Abs(remaining) > 0.5 {
		dir := 1.0
		if remaining < 0.0 {
			dir = -1.0
		}
		cD := math.Abs(remaining) * uniform(0.88, 1.02)
		cS := uniform(cfg.CorrectionSigmaMin, cfg.CorrectionSigmaMax)
		cPeak := mt * uniform(0.12, 0.18)
		corrections = append(corrections, sdCorrection{
			D:     cD,
			t0:    mt * uniform(0.55, 0.68),
			mu:    math.Log(cPeak) + cS*cS,
			sigma: cS,
			dirX:  tx * dir,
			dirY:  ty * dir,
		})

		left := remaining - cD*dir
		if math.Abs(left) > 0.3 && uniform(0.0, 1.0) < cfg.SecondCorrectionProb {
			d2 := 1.0
			if left < 0.0 {
				d2 = -1.0
			}
			cD2 := math.Abs(left) * uniform(0.85, 1.05)
			cS2 := uniform(0.10, 0.16)
			cP2 := mt * uniform(0.08, 0.12)
			corrections = append(corrections, sdCorrection{
				D:     cD2,
				t0:    mt * uniform(0.78, 0.88),
				mu:    math.Log(cP2) + cS2*cS2,
				sigma: cS2,
				dirX:  tx * d2,
				dirY:  ty * d2,
			})
		}
	}

	// ── Curvature arc ───────────────────────────────────────────
	curvAmp := distance * cfg.CurvatureScale * sdDirectionFactor(direction) * normal(0.0, 1.0)

	// ── Tremor parameters ───────────────────────────────────────
	tremorFreq := uniform(cfg.TremorFreqMin, cfg.TremorFreqMax)
	tremorAmp := uniform(cfg.TremorAmpMin, cfg.TremorAmpMax)
	tphX := uniform(0.0, 2.0*math.Pi)
	tphY := uniform(0.0, 2.0*math.Pi)

	// ── OU drift state ──────────────────────────────────────────
	ouX := 0.0
	ouY := 0.0

	// ── Sample times (gamma-distributed intervals) ──────────────
	totalT := mt * 1.15
	gScale := cfg.SampleDtMean / cfg.GammaShape

	times := []float64{0.0}
	for t := 0.0; t < totalT; {
		dt := gammaDist(cfg.GammaShape, gScale)
		dt = sdClamp(dt, 2.0, 25.0)
		t += dt
		if t <= totalT+15.0 {
			times = append(times, t)
		}
	}

	// ── Build trajectory ────────────────────────────────────────
	result := make([]TrajectoryPoint, 0, len(times))

	for i, t := range times {
		dtMs := cfg.SampleDtMean
		if i > 0 {
			dtMs = t - times[i-1]
		}
		dtS := dtMs / 1000.0

		// Primary lognormal progress [0..1].
		s := sdLognormalCDF(t, 0.0, primaryMu, primarySigma)

		// Base position along the tangent.
		bx := x0 + tx*primaryD*s
		by := y0 + ty*primaryD*s

		// Lateral curvature arc.
		bx += nx * curvAmp * sdCurvatureProfile(s)
		by += ny * curvAmp * sdCurvatureProfile(s)

		// Correction sub-movements.
		for _, c := range corrections {
			cs := sdLognormalCDF(t, c.t0, c.mu, c.sigma)
			bx += c.dirX * c.D * cs
			by += c.dirY * c.D * cs
		}

		// Instantaneous speed for noise modulation.
		speed := primaryD * sdLognormalPDF(t, 0.0, primaryMu, primarySigma)
		for _, c := range corrections {
			speed += c.D * sdLognormalPDF(t, c.t0, c.mu, c.sigma)
		}

		// Ornstein-Uhlenbeck lateral drift (Euler-Maruyama step).
		ouX += -cfg.OUTheta*ouX*dtS + cfg.OUSigma*math.Sqrt(dtS)*normal(0.0, 1.0)
		ouY += -cfg.OUTheta*ouY*dtS + cfg.OUSigma*math.Sqrt(dtS)*normal(0.0, 1.0)

		// Speed-modulated physiological tremor (8-12 Hz, suppressed during fast movement).
		tS := t / 1000.0
		tremMod := 1.0 / (1.0 + speed*0.3)
		trX := tremorAmp * tremMod * math.Sin(2.0*math.Pi*tremorFreq*tS+tphX)
		trY := tremorAmp * tremMod * math.Sin(2.0*math.Pi*tremorFreq*tS+tphY)

		// Signal-dependent noise (Harris-Wolpert SDN).
		sdnX := cfg.SDNK * speed * normal(0.0, 1.0)
		sdnY := cfg.SDNK * speed * normal(0.0, 1.0)

		result = append(result, TrajectoryPoint{
			X: bx + ouX + trX + sdnX,
			Y: by + ouY + trY + sdnY,
			T: t,
		})
	}

	return result
}

// sdGammaVariate generates a gamma-distributed random variate using
// Marsaglia and Tsang's method.
func sdGammaVariate(rng *rand.Rand, shape float64) float64 {
	if shape < 1.0 {
		return sdGammaVariate(rng, shape+1.0) * math.Pow(rng.Float64(), 1.0/shape)
	}

	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)

	for {
		var x, v float64
		for {
			x = rng.NormFloat64()
			v = 1.0 + c*x
			if v > 0.0 {
				break
			}
		}
		v = v * v * v
		u := rng.Float64()

		if u < 1.0-0.0331*(x*x)*(x*x) {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return d * v
		}
	}
}

// sdClamp restricts v to the range [lo, hi].
func sdClamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
