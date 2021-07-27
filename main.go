package main

import (
  "context"
  "flag"
  "fmt"
  "net/http"
  "os"
  "io/ioutil"

  "gopkg.in/yaml.v3"

  metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

  "github.com/sirupsen/logrus"
  kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
  kwhlog "github.com/slok/kubewebhook/v2/pkg/log"
  kwhlogrus "github.com/slok/kubewebhook/v2/pkg/log/logrus"
  kwhmodel "github.com/slok/kubewebhook/v2/pkg/model"
  kwhmutating "github.com/slok/kubewebhook/v2/pkg/webhook/mutating"
)

type config struct {
  Rules map[string]map[string]string
}

type annotationMutator struct {
  logger kwhlog.Logger
  config config
}

func (mutator *annotationMutator) Mutate(_ context.Context, _ *kwhmodel.AdmissionReview, obj metav1.Object) (*kwhmutating.MutatorResult, error) {
  rules := mutator.config.Rules

  lg := mutator.logger.WithValues(kwhlog.Kv{
    "namespace": obj.GetNamespace(),
    "name": obj.GetName(),
  })

  labels := obj.GetLabels()

  ruleName, ok := labels["resource-annotator.fehe.eu/rule"]
  if !ok {
    lg.Infof("No rule label. Skip.")
    return &kwhmutating.MutatorResult{}, nil
  }

  newAnnotations, ok := rules[ruleName]
  if !ok {
    lg.Warningf("No rule matching: %s. Skip.", ruleName)
    return &kwhmutating.MutatorResult{}, nil
  }

  annotations := obj.GetAnnotations()
  for k, v := range newAnnotations {
    lg.Debugf("Setting annotation %s=%s", k, v)
    annotations[k] = v
  }

  obj.SetAnnotations(annotations)

  return &kwhmutating.MutatorResult{
    MutatedObject: obj,
  }, nil
}

type flags struct {
  certFile   string
  keyFile    string
  configFile string
}

func initFlags() *flags {
  cfg := &flags{}

  fl := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
  fl.StringVar(&cfg.certFile, "tls-cert-file", "", "TLS certificate file")
  fl.StringVar(&cfg.keyFile, "tls-key-file", "", "TLS key file")
  fl.StringVar(&cfg.configFile, "config", "", "YAML file containing configuration")

  fl.Parse(os.Args[1:])
  return cfg
}

func loadConfig(flg *flags) (*config, error) {
  buf, err := ioutil.ReadFile(flg.configFile)
  if err != nil {
    return nil, err
  }

  cfg := &config{}
  err = yaml.Unmarshal(buf, cfg)
  if err != nil {
    return nil, fmt.Errorf("in file %q: %v", flg.configFile, err)
  }

  return cfg, nil
}

func main() {
  logrusLogEntry := logrus.NewEntry(logrus.New())
  logrusLogEntry.Logger.SetLevel(logrus.DebugLevel)
  logger := kwhlogrus.NewLogrus(logrusLogEntry)

  flg := initFlags()

  cfg, err := loadConfig(flg)
  if err != nil {
    fmt.Fprintf(os.Stderr, "error loading configuration: %s", err)
    os.Exit(1)
  }

  logger.Infof("%d rule(s) loaded.", len(cfg.Rules))

  mt := &annotationMutator{logger: logger, config: *cfg}

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
  err = http.ListenAndServeTLS(":8080", flg.certFile, flg.keyFile, whHandler)
  if err != nil {
    fmt.Fprintf(os.Stderr, "error serving webhook: %s", err)
    os.Exit(1)
  }
}
