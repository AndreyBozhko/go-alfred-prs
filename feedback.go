package main

import (
	"log"
	"strings"

	aw "github.com/deanishe/awgo"
	kc "github.com/deanishe/awgo/keychain"
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

// HandleError converts workflow errors to Alfred feedback items.
func (wf *GithubWorkflow) HandleError(e error) {
	if upd, ok := e.(*retryableError); ok && upd.attemptsLeft > 0 {
		wf.LaunchUpdateTask(upd)
		return
	}

	wf.Var(fbErrorOccurredKey, "true")

	switch e {
	case kc.ErrNotFound:
		wf.HandleMissingToken()
	default:
		wf.FatalError(e)
	}
}

// HandleMissingToken indicates to user that the API token is not set.
func (wf *GithubWorkflow) HandleMissingToken() {
	wf.NewWarningItem("No API key set", "Please use ghpr-auth to set you GitHub personal token")

	tokenUrl := wf.GetBaseWebUrl() + "/settings/tokens/new"
	wf.NewItem("Generate new token on GitHub").
		Subtitle(tokenUrl).
		Arg(tokenUrl + "?description=go-ghpr&scopes=repo").
		Valid(true).
		Icon(aw.IconWeb)
}
