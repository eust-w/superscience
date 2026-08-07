package release

import (
	"fmt"
	"strconv"
	"strings"
)

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease []string
}

func updateAvailable(current, latest string) (bool, error) {
	latestVersion, err := parseVersion(latest)
	if err != nil {
		return false, fmt.Errorf("latest version: %w", err)
	}
	current = strings.TrimSpace(current)
	if current == "" || strings.EqualFold(current, "dev") {
		return true, nil
	}
	currentVersion, err := parseVersion(current)
	if err != nil {
		return false, fmt.Errorf("current version: %w", err)
	}
	return compareVersion(currentVersion, latestVersion) < 0, nil
}

func parseVersion(value string) (semanticVersion, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if plus := strings.IndexByte(value, '+'); plus >= 0 {
		value = value[:plus]
	}
	core := value
	var prerelease []string
	if dash := strings.IndexByte(value, '-'); dash >= 0 {
		core = value[:dash]
		if dash == len(value)-1 {
			return semanticVersion{}, fmt.Errorf("invalid semantic version %q", value)
		}
		prerelease = strings.Split(value[dash+1:], ".")
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semanticVersion{}, fmt.Errorf("invalid semantic version %q", value)
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semanticVersion{}, fmt.Errorf("invalid semantic version %q", value)
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semanticVersion{}, fmt.Errorf("invalid semantic version %q", value)
		}
		numbers[index] = number
	}
	for _, identifier := range prerelease {
		if identifier == "" {
			return semanticVersion{}, fmt.Errorf("invalid semantic version %q", value)
		}
		for _, character := range identifier {
			if (character < '0' || character > '9') &&
				(character < 'A' || character > 'Z') &&
				(character < 'a' || character > 'z') &&
				character != '-' {
				return semanticVersion{}, fmt.Errorf("invalid semantic version %q", value)
			}
		}
	}
	return semanticVersion{
		major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: prerelease,
	}, nil
}

func compareVersion(left, right semanticVersion) int {
	for _, pair := range [][2]int{
		{left.major, right.major},
		{left.minor, right.minor},
		{left.patch, right.patch},
	} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) == 0 {
		return 0
	}
	if len(left.prerelease) == 0 {
		return 1
	}
	if len(right.prerelease) == 0 {
		return -1
	}
	for index := 0; index < len(left.prerelease) && index < len(right.prerelease); index++ {
		if comparison := comparePrereleaseIdentifier(left.prerelease[index], right.prerelease[index]); comparison != 0 {
			return comparison
		}
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1
	}
	return 0
}

func comparePrereleaseIdentifier(left, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	if leftErr == nil && rightErr == nil {
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
	}
	if leftErr == nil {
		return -1
	}
	if rightErr == nil {
		return 1
	}
	return strings.Compare(left, right)
}
