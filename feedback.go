package main

import (
	"log"
	"strings"

	aw "github.com/deanishe/awgo"
)

// AlfredMessage is a two-part message which can be
// displayed by Alfred as title and subtitle.
type AlfredMessage interface {
	Parts() (title, subtitle string)
}

// alfredError wraps an error and enables splitting
// its message into title and subtitle.
type alfredError struct {
	wrapped error
}

func (e *alfredError) Error() string {
	return e.wrapped.Error()
}

func (e *alfredError) Parts() (string, string) {
	msg := e.Error()
	idx := strings.LastIndex(msg, ": ")

	if len(msg) < 60 || idx < 0 {
		return msg, ""
	}

	return msg[idx+2:], msg[:idx]
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

func (e *retryableError) Parts() (string, string) {
	return e.message, e.hint
}

// FatalError overrides the default workflow handling of errors.
func (wf *GithubWorkflow) FatalError(e error) {
	am, ok := e.(AlfredMessage)
	if !ok {
		am = &alfredError{e}
	}

	title, subtitle := am.Parts()

	wf.Feedback.Clear()
	wf.NewItem(title).
		Subtitle(subtitle).
		Icon(aw.IconError)
	wf.SendFeedback()

	log.Printf("[ERROR] %s", e.Error())
}

// InfoEmpty adds an info item to feedback if there are no other items.
func (wf *GithubWorkflow) InfoEmpty(title, subtitle string) {
	if !wf.IsEmpty() {
		return
	}

	wf.NewItem(title).
		Subtitle(subtitle).
		Icon(aw.IconInfo)

	wf.SendFeedback()
}
