package poller

import (
	"math"
	"time"
)

// shortWindow is the width of the short burn-rate window (BURN-01..05).
// It drives detection and recovery: a service is classified degraded/outage
// only while the short window is burning, and returns to operational on the
// first cycle whose short window has traffic and is not burning - the same
// short-window-driven recovery as Datadog's multi-window `slo alert`
// monitor (1h/5m). In the 2026-10-22 replay (7 days, 8 metric-based SLOs at
// 99.5%/30d, 120s poll cycles), this pairing recovered on the same cycle as
// the monitor proxy on every SLO.
const shortWindow = 5 * time.Minute

// longWindow is the width of the long burn-rate window. Requiring the long
// window to burn as well filters out short bursts that do not threaten the
// error budget; it is the 1h half of Datadog's 1h/5m multi-window pairing,
// replayed on 2026-10-22 with 0 false episodes on every sampled SLO.
const longWindow = time.Hour

// outageBurnRate is the burn rate both windows must reach for "outage"
// (BURN-02): 14.4x consumes 2% of a 30-day budget in 1 hour, Datadog's
// multi-window critical threshold. In the 2026-10-22 replay it caught every
// real incident on the first cycle.
const outageBurnRate = 14.4

// degradedBurnRate is the burn rate both windows must reach for "degraded"
// (BURN-03): 7.2x, Datadog's multi-window warning threshold. In the
// 2026-10-22 replay the high-traffic gateway with a ~1% baseline error rate
// (burn ~2x at 99.5%) produced 0 false episodes at this threshold, against
// 714 non-operational minutes per week under the previous classifier.
const degradedBurnRate = 7.2

// minShortWindowErrors is the minimum number of failed requests the short
// window must contain before it can be classified degraded/outage (BURN-05).
// Swept in the 2026-10-22 replay: 1 and 2 let through noise on a mid-traffic
// service (2 errors in ~25 requests); 5 delayed or missed small real
// incidents (4 errors in 34 requests, 6 in 13). 3 suppresses only episodes
// with 1-2 errors in the short window, which is the intended effect.
const minShortWindowErrors = 3

// zeroTrafficGrace is how long a degraded/outage service keeps its status
// while its short window carries no traffic, measured from the last cycle
// whose short window had a failed request (BURN-06). 10/15/30 minutes made
// no difference in the 2026-10-22 replay, because production services
// always exit an incident through a window with traffic and no errors; the
// grace only acts on low-traffic environments (staging). 15 minutes is the
// decided value and is NOT validated by the replay.
const zeroTrafficGrace = 15 * time.Minute

// recoveryHold is how long degraded/outage is kept after the short window
// stops burning. It is 0 - no hold - and classifyBurn deliberately does not
// read it; the constant records the decision. In the 2026-10-22 replay the
// gateway had 30 episodes / 76 transitions per week at 0 min, 16 / 34 at
// 10 min and 5 / 12 at 20 min, at the cost of slower recovery than the spec
// goal (~8 min). The Datadog monitor proxy itself had 76 transitions on the
// gateway, so 0 matches Datadog exactly.
const recoveryHold time.Duration = 0

// windowCounts is the request/error count of one burn-rate window. It is
// local to the poller so the classifier stays independent of the connector
// type it is mapped from.
type windowCounts struct {
	Requests, Errors int64
}

// burnRate returns how many times faster than allowed a window consumes the
// error budget of an SLO whose target is target (in percent):
//
//	burn = (errors / requests) / (1 - target/100)
//
// The function is total over its inputs. A non-positive requests has no
// sample and returns 0. errors is clamped to [0, requests] first, so a
// metric aggregation skew where errors exceed requests caps the burn at
// 1 / (1 - target/100) (BURN-17). A target >= 100 has a zero budget: any
// error is +Inf, no errors is 0, without dividing by zero.
func burnRate(errors, requests int64, target float64) float64 {
	if requests <= 0 {
		return 0
	}
	if errors < 0 {
		errors = 0
	}
	if errors > requests {
		errors = requests
	}

	budget := 1 - target/100
	if budget <= 0 {
		if errors > 0 {
			return math.Inf(1)
		}
		return 0
	}

	return (float64(errors) / float64(requests)) / budget
}

// burnInput is everything classifyBurn needs to decide a service's status
// for one poll cycle. LastErrorAt is the time of the last cycle whose short
// window contained a failed request (nil if none is known); LastChangeAt is
// the time of the service's last status change. Both come from persisted
// data, so the zero-traffic grace survives restarts and HA handover.
type burnInput struct {
	Short, Long  windowCounts
	Target       float64
	Previous     string
	LastErrorAt  *time.Time
	LastChangeAt time.Time
	Now          time.Time
}

// classifyBurn returns the status ("operational", "degraded", "outage" or
// "not_configured") for one poll cycle, applying in order:
//
//  1. Short window without requests (no evidence, BURN-08):
//     - previous "not_configured": classify from the long window alone,
//     or stay "not_configured" if it is empty too (BURN-10);
//     - previous "outage"/"degraded": keep it until zeroTrafficGrace has
//     passed since LastErrorAt (or LastChangeAt when nil), then
//     "operational" (BURN-06);
//     - anything else: "operational" (BURN-07).
//  2. Long window without requests: long burn = short burn (BURN-17).
//  3. Short errors >= minShortWindowErrors and both burns >=
//     outageBurnRate: "outage" (BURN-02).
//  4. Short errors >= minShortWindowErrors and both burns >=
//     degradedBurnRate: "degraded" (BURN-03).
//  5. Otherwise "operational" (BURN-04, BURN-05): recovery is driven by
//     the short window alone, with no hold (see recoveryHold).
//
// It is pure: no I/O, no clock reads, no in-memory state across cycles.
func classifyBurn(in burnInput) string {
	if in.Short.Requests <= 0 {
		switch in.Previous {
		case "not_configured":
			if in.Long.Requests <= 0 {
				return "not_configured"
			}
			return classifyWindows(in.Long, in.Long, in.Target)
		case "outage", "degraded":
			since := in.LastChangeAt
			if in.LastErrorAt != nil {
				since = *in.LastErrorAt
			}
			if in.Now.Sub(since) >= zeroTrafficGrace {
				return "operational"
			}
			return in.Previous
		default:
			return "operational"
		}
	}

	return classifyWindows(in.Short, in.Long, in.Target)
}

// classifyWindows applies rules 2-5 of classifyBurn to a short window that
// has requests.
func classifyWindows(short, long windowCounts, target float64) string {
	shortBurn := burnRate(short.Errors, short.Requests, target)
	longBurn := shortBurn
	if long.Requests > 0 {
		longBurn = burnRate(long.Errors, long.Requests, target)
	}

	// Clamped the same way burnRate clamps, so a skewed window cannot pass
	// the minimum-errors guard with errors it does not actually have.
	shortErrors := min(max(short.Errors, 0), short.Requests)
	if shortErrors < minShortWindowErrors {
		return "operational"
	}

	switch {
	case shortBurn >= outageBurnRate && longBurn >= outageBurnRate:
		return "outage"
	case shortBurn >= degradedBurnRate && longBurn >= degradedBurnRate:
		return "degraded"
	default:
		return "operational"
	}
}
