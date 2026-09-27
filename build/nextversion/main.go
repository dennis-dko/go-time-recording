package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	latest := ""
	if len(os.Args) > 1 {
		latest = os.Args[1]
	}

	version, err := next(latest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(version)
}

// highest is the largest a minor or patch number is counted to before it hands
// on to the part before it.
const highest = 99

// next is the version after latest.
//
// Each part counts to highest and then hands on: after x.y.99 comes x.(y+1).0,
// and after x.99.99 comes (x+1).0.0. Counting the patch without a bound produced
// v0.2.100 - a number people have to read carefully, and one that reads as a
// hundred fixes into a line rather than as a line that should have been closed.
// A tag already past the bound hands on the same way instead of going to 101.
//
// With no tag at all the first release is v0.1.0, which says "released and not
// finished", which is true.
func next(latest string) (string, error) {
	latest = strings.TrimSpace(latest)
	if latest == "" {
		return "v0.1.0", nil
	}

	major, minor, patch, err := parse(latest)
	if err != nil {
		return "", err
	}

	patch++

	if patch > highest {
		minor, patch = minor+1, 0
	}

	if minor > highest {
		major, minor = major+1, 0
	}

	return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
}

// parse reads vMAJOR.MINOR.PATCH and nothing else.
func parse(version string) (major, minor, patch int, err error) {
	fields := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if !strings.HasPrefix(version, "v") || len(fields) != 3 {
		return 0, 0, 0, fmt.Errorf("%q is not a version of the form vMAJOR.MINOR.PATCH", version)
	}

	numbers := make([]int, 3)

	for i, field := range fields {
		n, convErr := strconv.Atoi(field)
		if convErr != nil || n < 0 {
			return 0, 0, 0, fmt.Errorf("%q is not a version of the form vMAJOR.MINOR.PATCH", version)
		}

		numbers[i] = n
	}

	return numbers[0], numbers[1], numbers[2], nil
}
