package utils

import "strings"

// ParseImageName extracts the image name from a full image string (e.g., "nginx:1.21" -> "nginx")
func ParseImageName(imageStr string) string {
	lastColonIndex := strings.LastIndex(imageStr, ":")
	if lastColonIndex == -1 {
		return imageStr
	}
	return imageStr[:lastColonIndex]
}

// ParseImageTag extracts the tag from a full image string (e.g., "nginx:1.21" -> "1.21")
// If no tag is present, returns "latest"
func ParseImageTag(imageStr string) string {
	lastColonIndex := strings.LastIndex(imageStr, ":")
	if lastColonIndex == -1 {
		return "latest"
	}
	return imageStr[lastColonIndex+1:]
}
