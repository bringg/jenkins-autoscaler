package gce_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bringg/jenkins-autoscaler/pkg/backend/type/gce"
)

var _ = Describe("parseURLPath", func() {
	It("test functionality", func() {
		type test struct {
			input   string
			pattern string
			want    map[string]string
		}

		tests := []test{
			{
				input:   "/compute/v1/projects/bringg-test/zones/us-east1-b/instances/integration-jenkins-slave-xpx5",
				pattern: "/compute/v1/projects/{projectID}/zones/{zoneID}/instances/{instanceName}",
				want: map[string]string{
					"projectID":    "bringg-test",
					"zoneID":       "us-east1-b",
					"instanceName": "integration-jenkins-slave-xpx5",
				},
			},
			{
				input:   "",
				pattern: "/{id}/foo",
				want:    map[string]string{},
			},
			{
				input:   "",
				pattern: "",
				want:    map[string]string{},
			},
			{
				input:   "https://www.googleapis.com/compute/v1/projects/bringg-test/zones/us-east1-b/instances/integration-jenkins-slave-xpx5",
				pattern: "/compute/v1/projects/{projectID}/zones/{zoneID}/instances/{instanceName}",
				want: map[string]string{
					"projectID":    "bringg-test",
					"zoneID":       "us-east1-b",
					"instanceName": "integration-jenkins-slave-xpx5",
				},
			},
			{
				input:   "http://foo.com/bad-url/123/poo/55",
				pattern: "/good-url/{id}/poo/{ty}",
				want:    map[string]string{},
			},
			{
				input:   "/foo/poo322?foo=poo&poo=foo",
				pattern: "/foo/{id}",
				want: map[string]string{
					"id": "poo322",
				},
			},
			{
				input:   "http://foo.com/foo/poo322/test?foo=poo&poo=foo",
				pattern: "/foo/{id}/{id2}",
				want: map[string]string{
					"id":  "poo322",
					"id2": "test",
				},
			},
		}

		for _, tc := range tests {
			Expect(gce.ParseURLPath(tc.pattern, tc.input)).To(Equal(tc.want))
		}
	})
})
