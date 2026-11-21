package regimes

import "slices"

// RulesFor returns the rule keys facts start, in site_work.json's order.
// validate rejects unless_rules cycles, so the recursion ends.
func (sw SiteWork) RulesFor(facts []string) []string {
	have := map[string]bool{}
	for _, f := range facts {
		have[f] = true
	}
	memo := map[string]bool{}
	var applies func(key string) bool
	applies = func(key string) bool {
		if v, ok := memo[key]; ok {
			return v
		}
		i := slices.IndexFunc(sw.Rules, func(r SiteWorkRule) bool { return r.RuleKey == key })
		if i < 0 {
			return false
		}
		r := sw.Rules[i]
		v := slices.ContainsFunc(r.WhenAny, func(group []string) bool {
			return len(group) > 0 && !slices.ContainsFunc(group, func(f string) bool { return !have[f] })
		})
		if v && slices.ContainsFunc(r.UnlessFacts, func(f string) bool { return have[f] }) {
			v = false
		}
		if v && slices.ContainsFunc(r.UnlessRules, applies) {
			v = false
		}
		memo[key] = v
		return v
	}

	keys := []string{}
	for _, r := range sw.Rules {
		if applies(r.RuleKey) {
			keys = append(keys, r.RuleKey)
		}
	}
	return keys
}

func (sw SiteWork) Fact(key string) (SiteWorkFact, bool) {
	for _, f := range sw.Facts {
		if f.Key == key {
			return f, true
		}
	}
	return SiteWorkFact{}, false
}
