// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package synthexec

import (
	"fmt"
	"time"

	"github.com/elastic/beats/v7/libbeat/beat/events"
	"github.com/elastic/beats/v7/libbeat/processors/add_data_stream"
	"github.com/elastic/elastic-agent-libs/mapstr"

	"github.com/gofrs/uuid"

	"github.com/elastic/beats/v7/heartbeat/eventext"
	"github.com/elastic/beats/v7/heartbeat/monitors/logger"
	"github.com/elastic/beats/v7/heartbeat/monitors/stdfields"
	"github.com/elastic/beats/v7/libbeat/beat"
)

type enricher func(event *beat.Event, se *SynthEvent) error

type streamEnricher struct {
	je      *journeyEnricher
	sFields stdfields.StdMonitorFields
}

func (senr *streamEnricher) enrich(event *beat.Event, se *SynthEvent) error {
	if senr.je == nil || (se != nil && se.Type == JourneyStart) {
		senr.je = newJourneyEnricher(senr)
	}

	return senr.je.enrich(event, se)
}

// journeyEnricher holds state across received SynthEvents retaining fields
// where relevant to properly enrich *beat.Event instances.
type journeyEnricher struct {
	journeyComplete bool
	journey         *Journey
	checkGroup      string
	errorCount      int
	firstError      error
	stepCount       int
	// The first URL we visit is the URL for this journey, which is set on the summary event.
	// We store the URL fields here for use on the summary event.
	urlFields      mapstr.M
	start          time.Time
	end            time.Time
	streamEnricher *streamEnricher
}

func newJourneyEnricher(senr *streamEnricher) *journeyEnricher {
	return &journeyEnricher{
		checkGroup:     makeUuid(),
		streamEnricher: senr,
	}
}

func makeUuid() string {
	u, err := uuid.NewV1()
	if err != nil {
		panic("Cannot generate v1 UUID, this should never happen!")
	}
	return u.String()
}

func (je *journeyEnricher) enrich(event *beat.Event, se *SynthEvent) error {
	if se == nil {
		return nil
	}

	if !se.Timestamp().IsZero() {
		event.Timestamp = se.Timestamp()
		// Record start and end so we can calculate journey duration accurately later
		switch se.Type {
		case JourneyStart:
			je.firstError = nil
			je.checkGroup = makeUuid()
			je.journey = se.Journey
			je.start = event.Timestamp
		case JourneyEnd, CmdStatus:
			je.end = event.Timestamp
		}
	} else {
		event.Timestamp = time.Now()
	}

	eventext.MergeEventFields(event, mapstr.M{
		"event.type": se.Type,
	})

	return je.enrichSynthEvent(event, se)
}

func (je *journeyEnricher) enrichSynthEvent(event *beat.Event, se *SynthEvent) error {
	var jobErr error
	if se.Error != nil {
		jobErr = stepError(se.Error)
		je.errorCount++
		if je.firstError == nil {
			je.firstError = jobErr
		}
	}

	switch se.Type {
	case CmdStatus:
		// If a command failed _after_ the journey was complete, as it happens
		// when an `afterAll` hook fails, for example, we don't wan't to include
		// a summary in the cmd/status event.
		if !je.journeyComplete {
			return je.createSummary(event)
		}
	case JourneyEnd:
		je.journeyComplete = true
		return je.createSummary(event)
	case "step/end":
		je.stepCount++
	case "step/screenshot":
		fallthrough
	case "step/screenshot_ref":
		fallthrough
	case "screenshot/block":
		add_data_stream.SetEventDataset(event, "browser.screenshot")
	case "journey/network_info":
		add_data_stream.SetEventDataset(event, "browser.network")
	}

	if se.Id != "" {
		event.SetID(se.Id)
		// This is only relevant for screenshots, which have a specific ID
		// In that case we always want to issue an update op
		_, _ = event.Meta.Put(events.FieldMetaOpType, events.OpTypeCreate)
	}

	eventext.MergeEventFields(event, se.ToMap())

	if je.urlFields == nil {
		if urlFields, err := event.GetValue("url"); err == nil {
			if ufMap, ok := urlFields.(mapstr.M); ok {
				je.urlFields = ufMap
			}
		}
	}
	return jobErr
}

func (je *journeyEnricher) createSummary(event *beat.Event) error {
	var up, down int
	if je.errorCount > 0 {
		up = 0
		down = 1
	} else {
		up = 1
		down = 0
	}

	// Incase of syntax errors or incorrect runner options, the Synthetics
	// runner would exit immediately with exitCode 1 and we do not set the duration
	// to inform the journey never ran
	if !je.start.IsZero() {
		duration := je.end.Sub(je.start)
		eventext.MergeEventFields(event, mapstr.M{
			"monitor": mapstr.M{
				"duration": mapstr.M{
					"us": duration.Microseconds(),
				},
			},
		})
	}
	eventext.MergeEventFields(event, mapstr.M{
		"url": je.urlFields,
		"event": mapstr.M{
			"type": "heartbeat/summary",
		},
		"synthetics": mapstr.M{
			"type":    "heartbeat/summary",
			"journey": je.journey,
		},
		"summary": mapstr.M{
			"up":   up,
			"down": down,
		},
	})

	_, err := event.GetValue("monitor.id")
	if err == nil {
		logger.LogRun(event, &je.stepCount)
	}

	if je.journeyComplete {
		return je.firstError
	}

	return fmt.Errorf("journey did not finish executing, %d steps ran", je.stepCount)
}

func stepError(e *SynthError) error {
	return fmt.Errorf("error executing step: %s", e.String())
}
