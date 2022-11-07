package main

import (
	"log"
	"strings"

	aw "github.com/deanishe/awgo"
)

// FeedbackError represents an
type FeedbackItem interface {
	Title() string
	Subtitle() string
}

// retryableError is an error that holds extra information such as number of remaining attempts.
type retryableError struct {
	message      string
	hint         string
	attemptsLeft int
}

func (e *retryableError) Error() string {
	return e.message + " - " + e.hint
}

func (e *retryableError) Title() string {
	return e.message
}

func (e *retryableError) Subtitle() string {
	return e.hint
}

// FatalError overrides the default workflow handling of errors.
func (wf *GithubWorkflow) FatalError(e error) {
	if fi, ok := e.(FeedbackItem); ok {
		wf.Feedback.Clear()
		wf.NewItem(fi.Title()).Subtitle(fi.Subtitle()).Icon(aw.IconError)
		wf.SendFeedback()

		log.Printf("[ERROR] %s", e.Error())
	}

	msg := e.Error()
	idx := strings.LastIndex(msg, ": ")

	if len(msg) >= 60 && idx >= 0 {
		title, subtitle := msg[idx+2:], msg[:idx]
		wf.Feedback.Clear()
		wf.NewItem(title).Subtitle(subtitle).Icon(aw.IconError)
		wf.SendFeedback()

		log.Printf("[ERROR] %s", e.Error())
	}

	wf.Workflow.Fatal(msg)
}

func (wf *GithubWorkflow) InfoEmpty(title, subtitle string) {
	if !wf.IsEmpty() {
		return
	}

	wf.Feedback.Clear()

	wf.NewItem(title).
		Subtitle(subtitle).
		Icon(aw.IconInfo)

	wf.SendFeedback()
}
