package controller

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Config samples validation", func() {
	It("all config/samples/ manifests pass server-side dry-run apply", func() {
		samplesDir := filepath.Join("..", "..", "config", "samples")
		entries, err := os.ReadDir(samplesDir)
		Expect(err).NotTo(HaveOccurred())

		applied := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") || entry.Name() == "kustomization.yaml" {
				continue
			}

			data, err := os.ReadFile(filepath.Join(samplesDir, entry.Name()))
			Expect(err).NotTo(HaveOccurred(), "reading %s", entry.Name())

			obj := &unstructured.Unstructured{}
			Expect(yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(data)), 1024).Decode(obj)).
				To(Succeed(), "decoding %s", entry.Name())

			if obj.GetNamespace() == "" {
				obj.SetNamespace("default")
			}

			err = k8sClient.Create(ctx, obj, client.DryRunAll)
			Expect(err).NotTo(HaveOccurred(), "dry-run apply failed for %s: server rejected the sample", entry.Name())
			applied++
		}
		Expect(applied).To(BeNumerically(">=", 4), "expected at least 4 sample manifests (one per CRD)")
	})
})
