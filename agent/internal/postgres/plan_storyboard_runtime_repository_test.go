package postgres

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
)

func TestClonePlanStoryboardElementsPreservesCanonicalEmptyCollections(t *testing.T) {
	values := []planstoryboard.Element{{
		Key: "element_1", DependencyKeys: []string{},
	}}

	cloned := clonePlanStoryboardElements(values)
	if cloned == nil || len(cloned) != 1 || cloned[0].DependencyKeys == nil {
		t.Fatalf("clone 必须保留 canonical []，不能把空集合改写成 null: %#v", cloned)
	}
	cloned[0].DependencyKeys = append(cloned[0].DependencyKeys, "dependency_1")
	if len(values[0].DependencyKeys) != 0 {
		t.Fatalf("clone 修改泄漏到原始值: original=%#v cloned=%#v", values, cloned)
	}
}
