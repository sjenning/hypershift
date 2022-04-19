package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/google/go-cmp/cmp"
	hyperapi "github.com/openshift/hypershift/api"
	hyperv1 "github.com/openshift/hypershift/api/v1alpha1"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	admissionv1 "k8s.io/api/admission/v1"
)

func main() {
	ctx := ctrl.SetupSignalHandler()

	restConfig := ctrl.GetConfigOrDie()
	restConfig.UserAgent = "admission-differ"
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:  hyperapi.Scheme,
		Port:    9443,
		CertDir: "/var/run/secrets/serving-cert",
	})
	if err != nil {
		log.Fatalf("unable to start manager: %s", err.Error())
	}

	hook := &diffTracer{}

	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/trace", &webhook.Admission{Handler: hook})

	err = mgr.Start(ctx)
	if err != nil {
		log.Fatalf("Start returned with error: %s", err.Error())
	}
}

type diffTracer struct {
	decoder *admission.Decoder
}

var _ admission.Handler = &diffTracer{}

func (v *diffTracer) Handle(_ context.Context, req admission.Request) admission.Response {
	var oldObj, newObj runtime.Object

	switch req.Kind.Kind {
	case "HostedCluster":
		oldObj, newObj = &hyperv1.HostedCluster{}, &hyperv1.HostedCluster{}
	case "AWSEndpointService":
		oldObj, newObj = &hyperv1.AWSEndpointService{}, &hyperv1.AWSEndpointService{}
	default:
		fmt.Printf("unsupported Kind %s\n", req.Kind)
		return admission.Allowed("")
	}

	err := v.decoder.Decode(req, newObj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	var output bytes.Buffer
	fmt.Fprintf(&output, "%s %s %s\n", newObj.GetObjectKind().GroupVersionKind().String(), req.Operation, req.UserInfo.Username)
	switch req.Operation {
	case admissionv1.Create:
		fmt.Fprintf(&output, "+%v", newObj)
	case admissionv1.Update:
		err = v.decoder.DecodeRaw(req.OldObject, oldObj)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		oldYaml, err := yaml.Marshal(oldObj)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		newYaml, err := yaml.Marshal(newObj)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		fmt.Fprint(&output, cmp.Diff(string(oldYaml), string(newYaml)))
	}
	fmt.Println(output.String())
	return admission.Allowed("")
}

var _ admission.DecoderInjector = &diffTracer{}

func (v *diffTracer) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
