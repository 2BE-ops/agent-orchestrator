package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type sendOptions struct {
	session         string
	message         string
	steer           bool
	clientMessageID string
	recoverOnly     bool
}

// sendAPIRequest mirrors the daemon's SendSessionMessageRequest body for
// POST /api/v1/sessions/{id}/send. The CLI keeps its own copy so it need not
// import httpd.
type sendAPIRequest struct {
	Message string `json:"message"`
}

// The steering DTOs mirror the daemon conversation API without coupling the
// thin CLI to the HTTP controller package.
type conversationMessageAPIRequest struct {
	Text            string `json:"text"`
	ClientMessageID string `json:"clientMessageId"`
	RecoverOnly     bool   `json:"recoverOnly,omitempty"`
}

type steerAPIResponse struct {
	ProviderTurnID string `json:"providerTurnId"`
}

type chatSendAPIResponse struct {
	TurnID         string `json:"turnId"`
	ProviderTurnID string `json:"providerTurnId"`
	State          string `json:"state"`
	Duplicate      bool   `json:"duplicate"`
}

type promoteQueuedTurnAPIResponse struct {
	SourceTurnID   string `json:"sourceTurnId"`
	ProviderTurnID string `json:"providerTurnId"`
}

func newSendCommand(ctx *commandContext) *cobra.Command {
	var opts sendOptions
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message to a running agent session",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.sendMessage(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.session, "session", "", "Session id (required)")
	cmd.Flags().StringVar(&opts.message, "message", "", "Message body (required unless --recover-only)")
	cmd.Flags().BoolVar(&opts.steer, "steer", false, "Steer an active Chat turn, or start the next turn when idle")
	cmd.Flags().StringVar(&opts.clientMessageID, "client-message-id", "", "Stable steering delivery handle")
	cmd.Flags().BoolVar(&opts.recoverOnly, "recover-only", false, "Recover a steering result without contacting the provider")
	return cmd
}

func (c *commandContext) sendMessage(ctx context.Context, opts sendOptions) error {
	if opts.recoverOnly && !opts.steer {
		return usageError{errors.New("usage: --recover-only requires --steer")}
	}
	if strings.TrimSpace(opts.clientMessageID) != "" && !opts.steer {
		return usageError{errors.New("usage: --client-message-id requires --steer")}
	}
	if opts.recoverOnly && strings.TrimSpace(opts.clientMessageID) == "" {
		return usageError{errors.New("usage: --recover-only requires --client-message-id")}
	}
	if !opts.recoverOnly && strings.TrimSpace(opts.message) == "" {
		return usageError{errors.New("usage: --message is required")}
	}
	message := opts.message
	if sender := strings.TrimSpace(os.Getenv("AO_SESSION_ID")); !opts.recoverOnly && sender != "" {
		message = "[from " + sender + "] " + message
	}
	session := strings.TrimSpace(opts.session)
	if session == "" {
		return usageError{errors.New("usage: --session is required")}
	}

	// PathEscape: session ids are already "-"/digit safe, but may later come
	// from sanitized issue refs; keep the URL well-formed regardless.
	path := "sessions/" + url.PathEscape(session)
	if !opts.steer {
		return c.postJSON(ctx, path+"/send", sendAPIRequest{Message: message}, nil)
	}
	return c.steerMessage(ctx, path, message, strings.TrimSpace(opts.clientMessageID), opts.recoverOnly)
}

func (c *commandContext) steerMessage(
	ctx context.Context,
	sessionPath, message, clientMessageID string,
	recoverOnly bool,
) error {
	if clientMessageID == "" {
		clientMessageID = uuid.NewString()
	}
	var steered steerAPIResponse
	err := c.postJSON(ctx, sessionPath+"/conversation/steer", conversationMessageAPIRequest{
		Text: message, ClientMessageID: clientMessageID, RecoverOnly: recoverOnly,
	}, &steered)
	if err == nil {
		if recoverOnly {
			_, _ = fmt.Fprintf(c.deps.Out,
				"Recovered steering receipt for active turn %s with delivery handle %s; agent action is not confirmed.\n",
				steered.ProviderTurnID, clientMessageID)
			return nil
		}
		_, _ = fmt.Fprintf(c.deps.Out,
			"Steering accepted by provider for active turn %s with delivery handle %s; agent action is not confirmed.\n",
			steered.ProviderTurnID, clientMessageID)
		return nil
	}

	var responseErr apiResponseError
	if errors.As(err, &responseErr) && responseErr.ErrorBody.Code == "CHAT_STEER_UNCERTAIN" {
		if recoverOnly {
			return fmt.Errorf("%w; delivery handle %s remains unresolved and was not resent", err, clientMessageID)
		}
		return fmt.Errorf(
			"%w; recover this delivery without resending it: ao send --session %s --steer --recover-only --client-message-id %s",
			err, strings.TrimPrefix(sessionPath, "sessions/"), clientMessageID)
	}
	if recoverOnly {
		return err
	}
	if !errors.As(err, &responseErr) || responseErr.ErrorBody.Code != "CHAT_NO_ACTIVE_TURN" {
		return err
	}

	var sent chatSendAPIResponse
	if err := c.postJSON(ctx, sessionPath+"/conversation/messages", conversationMessageAPIRequest{
		Text: message, ClientMessageID: clientMessageID,
	}, &sent); err != nil {
		return err
	}
	if sent.State == "queued" {
		if sent.TurnID == "" {
			return errors.New("daemon returned a queued Chat turn without a turn id")
		}
		var promoted promoteQueuedTurnAPIResponse
		if err := c.postJSON(ctx,
			sessionPath+"/conversation/turns/"+url.PathEscape(sent.TurnID)+"/steer",
			nil,
			&promoted,
		); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(c.deps.Out,
			"A turn started during fallback. Queued message %s was steered into active turn %s; agent action is not confirmed.\n",
			promoted.SourceTurnID, promoted.ProviderTurnID)
		return nil
	}
	if sent.Duplicate {
		_, _ = fmt.Fprintln(c.deps.Out, "Message was already accepted; delivery state is unchanged.")
		return nil
	}
	_, _ = fmt.Fprintf(c.deps.Out,
		"No active turn to steer. Message accepted as a normal Chat turn in %s state; provider delivery is not confirmed.\n",
		sent.State)
	return nil
}
