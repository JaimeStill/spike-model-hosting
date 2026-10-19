package gate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// admission judges the model on paper, from the profile and the router's build alone: it sends
// no request to a model. The model's licence is recorded, failing only when the profile lacks
// it; its origin is judged by STANDARDS (origin); it declares every role the set
// assigns it; its context is within its card's; the set's memory budget leaves the pool's floor
// free; and the router's build is at or above the profile's minimum.
func (r *Runner) admission(_ context.Context, rep *Report) []Check {
	m := r.model()
	if m == nil {
		return []Check{fact("profile entry", "", "the profile doesn't list "+r.Model, false)}
	}
	checks := []Check{
		fact("licence", "", orMissing(m.Licence), m.Licence != ""),
		origin(m),
	}

	var declared []string
	ok := true
	for _, role := range rep.Roles {
		pr := ProfileRole(role)
		if slices.Contains(m.Roles, pr) {
			declared = append(declared, role+" ("+pr+")")
		} else {
			ok = false
			declared = append(declared, role+" ("+pr+" not declared)")
		}
	}
	checks = append(checks, fact("roles declared", "", strings.Join(declared, ", "), ok))

	ctxCheck := measure("context", "", float64(m.C), "tokens", AtMost, float64(m.CardContext))
	ctxCheck.Detail = "the profile's c against the card's context"
	ctxCheck.Passed = ctxCheck.Passed && m.CardContext > 0
	checks = append(checks, ctxCheck)

	b := r.Set.MemoryBudget
	used, kind := b.MeasuredMiB, "measured at 4 in flight on "+b.Build
	if used == 0 {
		used, kind = b.EstimatedMiB, "estimated"
	}
	budget := measure("memory budget", "", float64(b.PoolMiB-used), "MiB free", AtLeast, r.Floors.Pool.FreeMiB)
	budget.Detail = fmt.Sprintf("set %s: %d of %d MiB used, %s", r.Set.Name, used, b.PoolMiB, kind)
	checks = append(checks, budget)

	have, err := buildNumber(rep.Build)
	if err != nil {
		return append(checks, broken("router build", "", err))
	}
	floor, err := buildNumber(r.Profile.Engine.MinBuild)
	if err != nil {
		return append(checks, broken("router build", "", err))
	}
	bc := measure("router build", "", float64(have), "", AtLeast, float64(floor))
	bc.Detail = rep.Build + " against the profile's min_build " + r.Profile.Engine.MinBuild
	return append(checks, bc)
}

// origin judges the model's origin by STANDARDS: a model of Chinese origin is chosen only when no
// alternative serves, and its entry records why. The check fails a missing origin, and a
// restricted one (profile.Model.OriginRestricted) whose entry records no origin_reason.
func origin(m *profile.Model) Check {
	detail := orMissing(m.Origin) + ", publisher " + orMissing(m.Publisher)
	switch {
	case m.Origin == "":
		return fact("origin", "", detail, false)
	case !m.OriginRestricted():
		return fact("origin", "", detail+"; unrestricted by STANDARDS", true)
	case m.OriginReason == "":
		return fact("origin", "", detail+"; STANDARDS chooses a model whose origin is "+profile.OriginChina+
			" only when no alternative serves, and the profile records no origin_reason", false)
	}
	return fact("origin", "", detail+"; chosen as no alternative serves: "+strings.Join(strings.Fields(m.OriginReason), " "), true)
}

func orMissing(s string) string {
	if s == "" {
		return "missing"
	}
	return s
}
