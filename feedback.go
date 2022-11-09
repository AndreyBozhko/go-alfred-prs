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

// alfredError stores the error message
// as title and subtitle.
type alfredError struct {
	title, subtitle string
}

func (e *alfredError) Error() string {
	return e.title + "\n" + e.subtitle
}

func (e *alfredError) Parts() (string, string) {
	return e.title, e.subtitle
}

// makeAlfredError creates an alfredError from any error
// by splitting its message into two parts.
func makeAlfredError(e error) *alfredError {
	msg := e.Error()
	idx := strings.LastIndex(msg, ": ")

	if len(msg) < 60 || idx < 0 {
		return &alfredError{msg, ""}
	}

	return &alfredError{msg[idx+2:], msg[:idx]}
}

// retryable is an error that holds extra information
// such as number of remaining attempts.
type retryable struct {
	message      string
	hint         string
	attemptsLeft int
}

func (e *retryable) Error() string {
	return e.message + " - " + e.hint
}

func (e *retryable) Parts() (string, string) {
	return e.message, e.hint
}

// FatalError overrides the default workflow handling of errors.
func (wf *GithubWorkflow) FatalError(e error) {
	am, ok := e.(AlfredMessage)
	if !ok {
		am = makeAlfredError(e)
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
	if upd, ok := e.(*retryable); ok && upd.attemptsLeft > 0 {
		wf.LaunchUpdateTask(upd.attemptsLeft)
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
