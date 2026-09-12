package poller

import "math"

// breachThresholdSigmas is how many standard errors the observed error rate
// may exceed the SLO's allowed error rate before pollService classifies a
// window as breached (AD-019 addendum 4). Datadog's own overall.state
// compares the window's SLI against the SLO's configured timeframe target
// (typically 99.5% over 30 days) without rescaling that target to the
// window width, so on a high-traffic service a single unlucky burst of
// errors - well inside normal variance for a 30-day budget - trips
// "breached" on its own. breachBound widens the comparison by the window's
// actual sampling variance, which is the root fix the AD-019 addendum 3
// hysteresis stopgap was standing in for.
//
// 3 is a conventional starting point (roughly a 0.13% false-breach rate per
// window if the true error rate equals the target). It is NOT calibrated
// against real Datadog traffic - this environment has no Datadog
// credential - so it ships as an uncalibrated constant, same posture as
// minRecentWindowRequests/recentWindowWidth/recentWindowLag. A future
// session with real data owns the calibration; it is a single constant,
// easy to tune, no migration.
const breachThresholdSigmas = 3.0

// breachBound returns the SLI below which a window carrying requestCount
// requests is a breach for an SLO whose configured target is target, using
// sigmas standard errors of tolerance. The bound is:
//
//	p0 = (100 - target) / 100
//	bound = 100 - 100 * (p0 + sigmas * sqrt(p0*(1-p0)/requestCount))
//
// i.e. the SLO's own allowed error rate, widened by the binomial standard
// error the window's sample size actually has. It is always <= target
// (sigmas*se >= 0) and converges to target as requestCount grows: the band
// removes sampling variance, not real degradation.
//
// The function is total over its inputs. A non-positive requestCount has no
// usable sample, so it returns target (no widening); a negative variance
// (target outside [0,100]) is clamped to zero rather than producing a NaN.
// Callers still apply their own Target<=0 and low-volume guards before
// relying on the result.
func breachBound(target float64, requestCount int64, sigmas float64) float64 {
	if requestCount <= 0 {
		return target
	}

	p0 := (100 - target) / 100
	variance := p0 * (1 - p0)
	if variance < 0 {
		variance = 0
	}

	se := math.Sqrt(variance / float64(requestCount))
	return 100 - 100*(p0+sigmas*se)
}
