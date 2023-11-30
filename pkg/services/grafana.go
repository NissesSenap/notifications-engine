package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	httputil "github.com/argoproj/notifications-engine/pkg/util/http"
	"google.golang.org/api/idtoken"

	log "github.com/sirupsen/logrus"
)

type GrafanaOptions struct {
	ApiUrl             string `json:"apiUrl"`
	ApiKey             string `json:"apiKey"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	GCPSaKey           string `json:"gcpSAKey"`
}

type grafanaService struct {
	opts GrafanaOptions
}

func NewGrafanaService(opts GrafanaOptions) NotificationService {
	return &grafanaService{opts: opts}
}

type GrafanaAnnotation struct {
	Time     int64    `json:"time"` // unix ts in ms
	IsRegion bool     `json:"isRegion"`
	Tags     []string `json:"tags"`
	Text     string   `json:"text"`
}

func (s *grafanaService) Send(notification Notification, dest Destination) error {
	ga := GrafanaAnnotation{
		Time:     time.Now().Unix() * 1000, // unix ts in ms
		IsRegion: false,
		Tags:     strings.Split(dest.Recipient, "|"),
		Text:     notification.Message,
	}

	if notification.Message == "" {
		log.Warnf("Message is an empty string or not provided in the notifications template")
	}

	client := &http.Client{}
	var err error
	if s.opts.GCPSaKey != "" {
		// client is a http.Client that automatically adds an "Authorization" header
		// to any requests made.
		ctx := context.Background()
		client, err = s.getGCPIAP(ctx)
		if err != nil {
			log.Errorf("Failed to setup GCP IAP client: %s", err)
			return err
		}
	}

	client = &http.Client{
		Transport: httputil.NewLoggingRoundTripper(
			httputil.NewTransport(s.opts.ApiUrl, s.opts.InsecureSkipVerify), log.WithField("service", "grafana")),
	}

	jsonValue, _ := json.Marshal(ga)
	apiUrl, err := url.Parse(s.opts.ApiUrl)

	if err != nil {
		return err
	}
	annotationApi := *apiUrl
	annotationApi.Path = path.Join(apiUrl.Path, "annotations")
	req, err := http.NewRequest("POST", annotationApi.String(), bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Errorf("Failed to create grafana annotation request: %s", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.opts.ApiKey))

	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("unable to read response data: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("request to %s has failed with error code %d : %s", s.opts.ApiUrl, response.StatusCode, string(data))
	}

	return err
}

func (s *grafanaService) getGCPIAP(ctx context.Context) (*http.Client, error) {
	client, err := idtoken.NewClient(ctx, s.opts.GCPSaKey)
	if err != nil {
		return nil, fmt.Errorf("idtoken.NewClient: %w", err)
	}
	return client, nil
}
