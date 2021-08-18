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

type rule struct {
  MatchNamespace string            `yaml:"matchNamespace"`
  MatchKind      string            `yaml:"matchKind"`
  MatchName      string            `yaml:"matchName"`
  MatchLabels    map[string]string `yaml:"matchLabels"`
  Annotations    map[string]string
}

type config struct {
  Rules []rule
}

type annotationMutator struct {
  logger kwhlog.Logger
  config config
}

func (mutator *annotationMutator) Mutate(_ context.Context, ar *kwhmodel.AdmissionReview, obj metav1.Object) (*kwhmutating.MutatorResult, error) {
  matched := false
  rules := mutator.config.Rules

  namespace := obj.GetNamespace()
  kind := ar.RequestGVK.Kind
  name := obj.GetName()

  lg := mutator.logger.WithValues(kwhlog.Kv{
    "namespace": namespace,
    "kind": kind,
    "name": name,
  })

  labels := obj.GetLabels()
  annotations := obj.GetAnnotations()

  for index, rule := range rules {
    if rule.MatchNamespace != "" && namespace != rule.MatchNamespace {
      lg.Debugf("Rule[%d] does not match namespace: %s!=%s", index, rule.MatchNamespace, namespace)
      continue
    }

    if rule.MatchKind != "" && kind != rule.MatchKind {
      lg.Debugf("Rule[%d] does not match kind: %s!=%s", index, rule.MatchKind, kind)
      continue
    }

    if rule.MatchName != "" && name != rule.MatchName {
      lg.Debugf("Rule[%d] does not match name: %s!=%s", index, rule.MatchName, name)
      continue
    }

    if len(rule.MatchLabels) > 0 {
      labelsMatched := true
      for k, v := range rule.MatchLabels {
        val, ok := labels[k]
        if !ok {
          lg.Debugf("Rule[%d] does not match: missing label %s", index, k)
          labelsMatched = false
          break
        }
        if val != v {
          lg.Debugf("Rule[%d] does not match label %s: %s!=%s ", index, k, v, val)
          labelsMatched = false
          break
        }
      }
      if !labelsMatched {
        continue
      }
    }

    lg.Debugf("Rule[%d] matched", index)
    matched = true
    if annotations == nil {
      annotations = make(map[string]string)
    }
    for k, v := range rule.Annotations {
      lg.Debugf("Setting annotation %s=%s", k, v)
      annotations[k] = v
    }
  }

  if !matched {
    lg.Warningf("No rule matched. Skip.")
    return &kwhmutating.MutatorResult{}, nil
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
