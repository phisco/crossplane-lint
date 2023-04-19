package rules

import (
	"context"
	"fmt"
	"github.com/crossplane-contrib/crossplane-lint/internal/xpkg"
	"github.com/crossplane-contrib/crossplane-lint/internal/xpkg/lint"
	"github.com/crossplane-contrib/crossplane-lint/internal/xpkg/lint/jsonpath"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane/pkg/validation/apiextensions/v1/composition"
	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sync"
)

func CheckCompositionSchemaAwareValidation(ctx lint.LinterContext, pkg *xpkg.Package) {
	wg := sync.WaitGroup{}
	wg.Add(len(pkg.Entries))

	for _, m := range pkg.Entries {
		manifest := m
		go func() {
			checkCompositionWithSchemas(ctx, pkg, manifest)
			wg.Done()
		}()
	}
	wg.Wait()

}

type crdGetter struct {
	lint.LinterContext
}

func (c crdGetter) Get(_ context.Context, gk schema.GroupKind) (*apiextensions.CustomResourceDefinition, error) {
	crd := c.LinterContext.GetCRDSchema(gk)
	if crd == nil {
		return nil, errors.Errorf("CRD %s not found", gk)
	}
	return crd, nil
}

func (c crdGetter) GetAll(_ context.Context) (map[schema.GroupKind]apiextensions.CustomResourceDefinition, error) {
	return c.LinterContext.GetAll(), nil
}

var _ composition.CRDGetter = &crdGetter{}

func checkCompositionWithSchemas(ctx lint.LinterContext, pkg *xpkg.Package, manifest xpkg.PackageEntry) {
	if !manifest.IsComposition() {
		return
	}
	comp, err := manifest.AsComposition()
	if err != nil {
		ctx.ReportIssue(lint.Issue{
			Entry:       &manifest,
			Description: errors.Wrapf(err, errConvertTo, "Composition").Error(),
		})
		return
	}
	compositeGv, err := schema.ParseGroupVersion(comp.Spec.CompositeTypeRef.APIVersion)
	if err != nil {
		ctx.ReportIssue(lint.Issue{
			Entry:       &manifest,
			Description: errors.Wrap(err, errParseGroupVersion).Error(),
		})
		return
	}
	compositeGvk := compositeGv.WithKind(comp.Spec.CompositeTypeRef.Kind)
	compositeCRD := ctx.GetCRDSchema(compositeGvk.GroupKind())
	if compositeCRD == nil {
		ctx.ReportIssue(lint.Issue{
			Entry:       &manifest,
			Path:        jsonpath.NewJSONPath("spec", "compositeTypeRef"),
			Description: errors.Errorf(errNoCRDForGVK, compositeGvk.String()).Error(),
		})
		return
	}

	v, err := composition.NewValidator(composition.WithCRDGetter(crdGetter{ctx}))
	if err != nil {
		ctx.ReportIssue(lint.Issue{
			Entry:       &manifest,
			Description: errors.Wrap(err, "creating schema aware validator").Error(),
		})
		return
	}
	warns, errs := v.Validate(context.Background(), comp)
	if len(warns) > 0 {
		for _, warn := range warns {
			fmt.Println(warn)
		}
	}
	if len(errs) > 0 {
		for _, err := range errs {
			ctx.ReportIssue(lint.Issue{
				RuleName:    "CompositionSchemaAwareValidation",
				Path:        toJSONPath(err),
				Entry:       &manifest,
				Description: err.Error(),
			})
		}
		return
	}
}

// toJSONPath converts a field.Path to a jsonpath.JSONPath.
func toJSONPath(e *field.Error) jsonpath.JSONPath {
	if e == nil || e.Field == "" {
		return jsonpath.JSONPath{}
	}
	segments, _ := fieldpath.Parse(e.Field)
	return jsonpath.NewJSONPath(segments)
}
