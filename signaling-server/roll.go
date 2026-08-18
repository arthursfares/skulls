package main

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
)

var diceNotationRe = regexp.MustCompile(`^(\d{1,2})d(\d{1,3})([+-]\d{1,3})?$`)

var standardDice = map[int]bool{4: true, 6: true, 8: true, 10: true, 12: true, 20: true, 100: true}

// NOTE. parseDiceNotation parses "NdM" dice notation with an optional flat
// modifier (e.g. "4d6", "3d6+2", "1d20-4"). count is capped and sides must
// be one of the standard dice (d4/d6/d8/d10/d12/d20/d100).
func parseDiceNotation(s string) (count, sides, modifier int, err error) {
	m := diceNotationRe.FindStringSubmatch(s)
	if m == nil { return 0, 0, 0, fmt.Errorf("expected NdM notation, e.g. 4d6 or 3d6+2") }
	count, _ = strconv.Atoi(m[1])
	sides, _ = strconv.Atoi(m[2])
	if m[3] != "" { modifier, _ = strconv.Atoi(m[3]) }
	if count < 1 || count > 100 { return 0, 0, 0, fmt.Errorf("dice count must be between 1 and 100") }
	if !standardDice[sides] { return 0, 0, 0, fmt.Errorf("sides must be one of d4, d6, d8, d10, d12, d20, d100") }
	return count, sides, modifier, nil
}

func rollDice(count, sides int) []int {
	results := make([]int, count)
	for i := range results { results[i] = rand.Intn(sides) + 1 }
	return results
}

func sumInts(nums []int) int {
	total := 0
	for _, n := range nums { total += n }
	return total
}
