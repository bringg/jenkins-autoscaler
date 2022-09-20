package gce

import (
	"net/url"
	"regexp"
)

// Used for matching variables in a request URL.
var reResVars = regexp.MustCompile(`\\\{[^{}]+\\\}`)

// ParseURLPath path can contain variables denoted by curly braces, such as {id}.
// These variables will be parsed and stored in map.
//
// Example:
//
//	pattern	= /compute/v1/projects/{projectID}/zones/{zoneID}/instances/{instanceName}
//	url		= /compute/v1/projects/bringg-test/zones/us-east1-b/instances/integration-jenkins-slave-xpx5
//
//	map = { "instanceName": "integration-jenkins-slave-xpx5", "projectID": "bringg-test", "zoneID": "us-east1-b" }
func ParseURLPath(pattern string, rawURL string) map[string]string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}

	pattern = regexp.QuoteMeta(pattern)
	pattern = reResVars.ReplaceAllStringFunc(pattern, func(s string) string {
		group := s[2 : len(s)-2]
		return "(?P<" + group + ">[\\w\\d-]+)"
	})

	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(u.Path)
	vars := make(map[string]string, len(matches))

	if matches != nil {
		names := re.SubexpNames()

		for i, match := range matches[1:] {
			vars[names[i+1]] = match
		}
	}

	return vars
}
