package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
)

var testTagExpression = regexp.MustCompile(`([+\-]?[\pL\pN_:]*)`)

type TestTagFilter struct {
	include map[string]bool
	exclude map[string]bool
}

func newTestTagFilter(mf *MakeFlags) TestTagFilter {
	filters := slices.Clone(mf.testTags)

	if value := os.Getenv("YA_TEST_TAG"); value != "" {
		filters = append(filters, value)
	}

	if (mf.dflags["AUTOCHECK"] == "yes" || mf.runTests < 3) && !slices.ContainsFunc(filters, func(value string) bool { return strings.Contains(value, "ya:not_autocheck") }) {
		filters = append(filters, "-ya:not_autocheck")
	}

	filter := TestTagFilter{include: map[string]bool{}, exclude: map[string]bool{}}

	for _, value := range filters {
		for _, expression := range testTagExpression.FindAllString(value, -1) {
			if expression == "" {
				continue
			}

			if expression[0] == '-' {
				filter.exclude[expression[1:]] = true
			} else {
				filter.include[strings.TrimPrefix(expression, "+")] = true
			}
		}
	}

	for tag := range filter.include {
		if filter.exclude[tag] {
			throwFmt("test tag include and exclude filters intersect: %s", tag)
		}
	}

	if !filter.include["ya:manual"] {
		filter.exclude["ya:manual"] = true
	}

	return filter
}

func (filter TestTagFilter) matches(tags []string) bool {
	if filter.include["ya:anytag"] {
		return true
	}

	if len(tags) == 0 {
		tags = []string{"ya:notags"}
	}

	match := len(filter.include) == 0

	for _, tag := range tags {
		if filter.exclude[tag] {
			return false
		}

		match = match || filter.include[tag]
	}

	return match
}
