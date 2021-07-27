package main

import (
  "context"
  "flag"
  "fmt"
  "net/http"
  "os"
  "strings"

  metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

  "github.com/sirupsen/logrus"
  kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
  kwhlog "github.com/slok/kubewebhook/v2/pkg/log"
  kwhlogrus "github.com/slok/kubewebhook/v2/pkg/log/logrus"
  kwhmodel "github.com/slok/kubewebhook/v2/pkg/model"
  kwhmutating "github.com/slok/kubewebhook/v2/pkg/webhook/mutating"
)

type annotationMutator struct {
  logger kwhlog.Logger
}

func (mutator *annotationMutator) Mutate(_ context.Context, _ *kwhmodel.AdmissionReview, obj metav1.Object) (*kwhmutating.MutatorResult, error) {
  labels := obj.GetLabels()
  mutated := false

  lg := mutator.logger.WithValues(kwhlog.Kv{
    "namespace": obj.GetNamespace(),
    "name": obj.GetName(),
  })

  annotations := obj.GetAnnotations()

  for k, v := range labels {
    if !strings.HasPrefix(k, "annotate-") {
      lg.Debugf("Label %s does not match.", k)
      continue
    }
    newKey := strings.TrimPrefix(k, "annotate-")
    lg.Debugf("Adding annotation: '%s'='%s'", newKey, v)
    annotations[newKey] = v

    mutated = true
  }

  if !mutated {
    lg.Infof("Not changing any annotations.")
    return &kwhmutating.MutatorResult{}, nil
  }

  obj.SetAnnotations(annotations)
  return &kwhmutating.MutatorResult{
    MutatedObject: obj,
  }, nil
}

type config struct {
  certFile string
  keyFile  string
}

func initFlags() *config {
  cfg := &config{}

  fl := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
  fl.StringVar(&cfg.certFile, "tls-cert-file", "", "TLS certificate file")
  fl.StringVar(&cfg.keyFile, "tls-key-file", "", "TLS key file")

  fl.Parse(os.Args[1:])
  return cfg
}

func main() {
  logrusLogEntry := logrus.NewEntry(logrus.New())
  logrusLogEntry.Logger.SetLevel(logrus.DebugLevel)
  logger := kwhlogrus.NewLogrus(logrusLogEntry)

  cfg := initFlags()

  mt := &annotationMutator{logger: logger}

  mcfg := kwhmutating.WebhookConfig{
    ID:      "annotateFromLabel",
    Mutator: mt,
    Logger:  logger,
  }
  wh, err := kwhmutating.NewWebhook(mcfg)
  if err != nil {
    fmt.Fprintf(os.Stderr, "error creating webhook: %s", err)
    os.Exit(1)
  }

  // Get the handler for our webhook.
  whHandler, err := kwhhttp.HandlerFor(kwhhttp.HandlerConfig{Webhook: wh, Logger: logger})
  if err != nil {
    fmt.Fprintf(os.Stderr, "error creating webhook handler: %s", err)
    os.Exit(1)
  }
  logger.Infof("Listening on :8080")
  err = http.ListenAndServeTLS(":8080", cfg.certFile, cfg.keyFile, whHandler)
  if err != nil {
    fmt.Fprintf(os.Stderr, "error serving webhook: %s", err)
    os.Exit(1)
  }
}
