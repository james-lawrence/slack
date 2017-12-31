package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const (
	DEFAULT_MESSAGE_USERNAME         = ""
	DEFAULT_MESSAGE_THREAD_TIMESTAMP = ""
	DEFAULT_MESSAGE_REPLY_BROADCAST  = false
	DEFAULT_MESSAGE_ASUSER           = false
	DEFAULT_MESSAGE_PARSE            = ""
	DEFAULT_MESSAGE_LINK_NAMES       = 0
	DEFAULT_MESSAGE_UNFURL_LINKS     = false
	DEFAULT_MESSAGE_UNFURL_MEDIA     = true
	DEFAULT_MESSAGE_ICON_URL         = ""
	DEFAULT_MESSAGE_ICON_EMOJI       = ""
	DEFAULT_MESSAGE_MARKDOWN         = true
	DEFAULT_MESSAGE_ESCAPE_TEXT      = true
)

type chatResponseFull struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"ts"`
	Text      string `json:"text"`
	SlackResponse
}

// PostMessageParameters contains all the parameters necessary (including the optional ones) for a PostMessage() request
type PostMessageParameters struct {
	Text            string       `json:"text"`
	Username        string       `json:"user_name"`
	AsUser          bool         `json:"as_user"`
	Parse           string       `json:"parse"`
	ThreadTimestamp string       `json:"thread_ts"`
	ReplyBroadcast  bool         `json:"reply_broadcast"`
	LinkNames       int          `json:"link_names"`
	Attachments     []Attachment `json:"attachments"`
	UnfurlLinks     bool         `json:"unfurl_links"`
	UnfurlMedia     bool         `json:"unfurl_media"`
	IconURL         string       `json:"icon_url"`
	IconEmoji       string       `json:"icon_emoji"`
	Markdown        bool         `json:"mrkdwn,omitempty"`
	EscapeText      bool         `json:"escape_text"`

	// chat.postEphemeral support
	Channel string `json:"channel"`
	User    string `json:"user"`
}

// NewPostMessageParameters provides an instance of PostMessageParameters with all the sane default values set
func NewPostMessageParameters() PostMessageParameters {
	return PostMessageParameters{
		Username:    DEFAULT_MESSAGE_USERNAME,
		User:        DEFAULT_MESSAGE_USERNAME,
		AsUser:      DEFAULT_MESSAGE_ASUSER,
		Parse:       DEFAULT_MESSAGE_PARSE,
		LinkNames:   DEFAULT_MESSAGE_LINK_NAMES,
		Attachments: nil,
		UnfurlLinks: DEFAULT_MESSAGE_UNFURL_LINKS,
		UnfurlMedia: DEFAULT_MESSAGE_UNFURL_MEDIA,
		IconURL:     DEFAULT_MESSAGE_ICON_URL,
		IconEmoji:   DEFAULT_MESSAGE_ICON_EMOJI,
		Markdown:    DEFAULT_MESSAGE_MARKDOWN,
		EscapeText:  DEFAULT_MESSAGE_ESCAPE_TEXT,
	}
}

// DeleteMessage deletes a message in a channel
func (api *Client) DeleteMessage(channel, messageTimestamp string) (string, string, error) {
	respChannel, respTimestamp, _, err := api.SendMessageContext(context.Background(), channel, MsgOptionDelete(messageTimestamp))
	return respChannel, respTimestamp, err
}

// DeleteMessageContext deletes a message in a channel with a custom context
func (api *Client) DeleteMessageContext(ctx context.Context, channel, messageTimestamp string) (string, string, error) {
	respChannel, respTimestamp, _, err := api.SendMessageContext(ctx, channel, MsgOptionDelete(messageTimestamp))
	return respChannel, respTimestamp, err
}

// PostMessage sends a message to a channel.
// Message is escaped by default according to https://api.slack.com/docs/formatting
// Use http://davestevens.github.io/slack-message-builder/ to help crafting your message.
func (api *Client) PostMessage(channel, text string, params PostMessageParameters) (string, string, error) {
	respChannel, respTimestamp, _, err := api.SendMessageContext(
		context.Background(),
		channel,
		MsgOptionText(text, params.EscapeText),
		MsgOptionAttachments(params.Attachments...),
		MsgOptionPostMessageParameters(params),
	)
	return respChannel, respTimestamp, err
}

// PostMessageContext sends a message to a channel with a custom context
// For more details, see PostMessage documentation
func (api *Client) PostMessageContext(ctx context.Context, channel, text string, params PostMessageParameters) (string, string, error) {
	respChannel, respTimestamp, _, err := api.SendMessageContext(
		ctx,
		channel,
		MsgOptionText(text, params.EscapeText),
		MsgOptionAttachments(params.Attachments...),
		MsgOptionPostMessageParameters(params),
	)
	return respChannel, respTimestamp, err
}

// PostEphemeral sends an ephemeral message to a user in a channel.
// Message is escaped by default according to https://api.slack.com/docs/formatting
// Use http://davestevens.github.io/slack-message-builder/ to help crafting your message.
func (api *Client) PostEphemeral(channel, userID string, options ...MsgOption) (string, error) {
	options = append(options, MsgOptionPostEphemeral())
	return api.PostEphemeralContext(
		context.Background(),
		channel,
		userID,
		options...,
	)
}

// PostEphemeralContext sends an ephemeal message to a user in a channel with a custom context
// For more details, see PostEphemeral documentation
func (api *Client) PostEphemeralContext(ctx context.Context, channel, userID string, options ...MsgOption) (string, error) {
	path, values, err := ApplyMsgOptions(api.token, channel, options...)
	if err != nil {
		return "", err
	}

	values.Add("user", userID)

	response, err := chatRequest(ctx, api.httpclient, path, values, api.debug)
	if err != nil {
		return "", err
	}

	return response.Timestamp, nil
}

// UpdateMessage updates a message in a channel
func (api *Client) UpdateMessage(channel, timestamp, text string) (string, string, string, error) {
	return api.UpdateMessageContext(context.Background(), channel, timestamp, text)
}

// UpdateMessageContext updates a message in a channel
func (api *Client) UpdateMessageContext(ctx context.Context, channel, timestamp, text string) (string, string, string, error) {
	return api.SendMessageContext(ctx, channel, MsgOptionUpdate(timestamp), MsgOptionText(text, true))
}

// SendMessage more flexible method for configuring messages.
func (api *Client) SendMessage(channel string, options ...MsgOption) (string, string, string, error) {
	return api.SendMessageContext(context.Background(), channel, options...)
}

// SendMessageContext more flexible method for configuring messages with a custom context.
func (api *Client) SendMessageContext(ctx context.Context, channel string, options ...MsgOption) (string, string, string, error) {
	var (
		err      error
		req      *http.Request
		response = &chatResponseFull{}
		parser   func(*chatResponseFull) responseParser
	)

	if req, parser, err = buildSender(options...).BuildRequest(api.token, channel); err != nil {
		return "", "", "", err
	}

	if err = post(ctx, api.httpclient, req, parser(response), api.debug); err != nil {
		return "", "", "", err
	}

	return response.Channel, response.Timestamp, response.Text, nil
}

// ApplyMsgOptions utility function for debugging/testing chat requests.
func ApplyMsgOptions(token, channel string, options ...MsgOption) (string, url.Values, error) {
	config, err := applyMsgOptions(token, channel, options...)
	return string(config.mode), config.values, err
}

func applyMsgOptions(token, channel string, options ...MsgOption) (sendConfig, error) {
	config := sendConfig{
		mode: chatPostMessage,
		values: url.Values{
			"token":   {token},
			"channel": {channel},
		},
	}

	for _, opt := range options {
		if err := opt(&config); err != nil {
			return config, err
		}
	}

	return config, nil
}

func buildSender(options ...MsgOption) sendConfig {
	return sendConfig{
		options: options,
	}
}

func escapeMessage(message string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(message)
}

type sendMode string

const (
	chatUpdate        sendMode = "chat.update"
	chatPostMessage   sendMode = "chat.postMessage"
	chatDelete        sendMode = "chat.delete"
	chatPostEphemeral sendMode = "chat.postEphemeral"
	chatResponse      sendMode = "chat.responseURL"
)

type sendConfig struct {
	options      []MsgOption
	mode         sendMode
	endpoint     string
	values       url.Values
	attachments  []Attachment
	responseType string
}

func (t sendConfig) BuildRequest(token, channel string) (*http.Request, func(*chatResponseFull) responseParser, error) {
	config, err := applyMsgOptions(token, channel, t.options...)
	if err != nil {
		return nil, nil, err
	}

	switch config.mode {
	case chatResponse:
		return responseURLSender{
			endpoint:     config.endpoint,
			values:       config.values,
			attachments:  config.attachments,
			responseType: config.responseType,
		}.BuildRequest()
	default:
		return formSender{endpoint: config.endpoint, values: config.values}.BuildRequest()
	}
}

type formSender struct {
	endpoint string
	values   url.Values
}

func (t formSender) BuildRequest() (*http.Request, func(*chatResponseFull) responseParser, error) {
	req, err := formReq(t.endpoint, t.values)
	return req, func(resp *chatResponseFull) responseParser {
		return newJSONResponseParser(resp)
	}, err
}

type responseURLSender struct {
	endpoint     string
	values       url.Values
	attachments  []Attachment
	responseType string
}

func (t responseURLSender) BuildRequest() (*http.Request, func(*chatResponseFull) responseParser, error) {
	req, err := jsonReq(t.endpoint, Msg{
		Text:         t.values.Get("text"),
		Timestamp:    t.values.Get("ts"),
		Attachments:  t.attachments,
		ResponseType: t.responseType,
	})
	return req, func(resp *chatResponseFull) responseParser {
		return newTextResponseParser(resp)
	}, err
}

// MsgOption option provided when sending a message.
type MsgOption func(*sendConfig) error

// MsgOptionPost posts a messages, this is the default.
func MsgOptionPost() MsgOption {
	return func(config *sendConfig) error {
		config.mode = chatPostMessage
		config.endpoint = SLACK_API + string(chatPostMessage)
		config.values.Del("ts")
		return nil
	}
}

// MsgOptionPostEphemeral posts an ephemeral message
func MsgOptionPostEphemeral() MsgOption {
	return func(config *sendConfig) error {
		config.mode = chatPostEphemeral
		config.values.Del("ts")
		return nil
	}
}

// MsgOptionUpdate updates a message based on the timestamp.
func MsgOptionUpdate(timestamp string) MsgOption {
	return func(config *sendConfig) error {
		config.mode = chatUpdate
		config.endpoint = SLACK_API + string(chatUpdate)
		config.values.Add("ts", timestamp)
		return nil
	}
}

// MsgOptionDelete deletes a message based on the timestamp.
func MsgOptionDelete(timestamp string) MsgOption {
	return func(config *sendConfig) error {
		config.mode = chatDelete
		config.endpoint = SLACK_API + string(chatDelete)
		config.values.Add("ts", timestamp)
		return nil
	}
}

// MsgOptionResponseURL supplies a url to use as the endpoint.
func MsgOptionResponseURL(url string, rt string) MsgOption {
	return func(config *sendConfig) error {
		config.mode = chatResponse
		config.endpoint = url
		config.responseType = rt
		config.values.Del("ts")
		return nil
	}
}

// MsgOptionAsUser whether or not to send the message as the user.
func MsgOptionAsUser(b bool) MsgOption {
	return func(config *sendConfig) error {
		if b != DEFAULT_MESSAGE_ASUSER {
			config.values.Set("as_user", "true")
		}
		return nil
	}
}

// MsgOptionText provide the text for the message, optionally escape the provided
// text.
func MsgOptionText(text string, escape bool) MsgOption {
	return func(config *sendConfig) error {
		if escape {
			text = escapeMessage(text)
		}
		config.values.Add("text", text)
		return nil
	}
}

// MsgOptionAttachments provide attachments for the message.
func MsgOptionAttachments(attachments ...Attachment) MsgOption {
	return func(config *sendConfig) error {
		if attachments == nil {
			return nil
		}

		config.attachments = attachments

		// FIXME: We are setting the attachments on the message twice: above for
		// the json version, and below for the html version.  The marshalled bytes
		// we put into config.values below don't work directly in the Msg version.

		attachmentBytes, err := json.Marshal(attachments)
		if err == nil {
			config.values.Set("attachments", string(attachmentBytes))
		}

		return err
	}
}

// MsgOptionEnableLinkUnfurl enables link unfurling
func MsgOptionEnableLinkUnfurl() MsgOption {
	return func(config *sendConfig) error {
		config.values.Set("unfurl_links", "true")
		return nil
	}
}

// MsgOptionDisableLinkUnfurl disables link unfurling
func MsgOptionDisableLinkUnfurl() MsgOption {
	return func(config *sendConfig) error {
		config.values.Set("unfurl_links", "false")
		return nil
	}
}

// MsgOptionDisableMediaUnfurl disables media unfurling.
func MsgOptionDisableMediaUnfurl() MsgOption {
	return func(config *sendConfig) error {
		config.values.Set("unfurl_media", "false")
		return nil
	}
}

// MsgOptionDisableMarkdown disables markdown.
func MsgOptionDisableMarkdown() MsgOption {
	return func(config *sendConfig) error {
		config.values.Set("mrkdwn", "false")
		return nil
	}
}

// MsgOptionPostMessageParameters maintain backwards compatibility.
func MsgOptionPostMessageParameters(params PostMessageParameters) MsgOption {
	return func(config *sendConfig) error {
		if params.Username != DEFAULT_MESSAGE_USERNAME {
			config.values.Set("username", string(params.Username))
		}

		// chat.postEphemeral support
		if params.User != DEFAULT_MESSAGE_USERNAME {
			config.values.Set("user", params.User)
		}

		// never generates an error.
		MsgOptionAsUser(params.AsUser)(config)

		if params.Parse != DEFAULT_MESSAGE_PARSE {
			config.values.Set("parse", string(params.Parse))
		}
		if params.LinkNames != DEFAULT_MESSAGE_LINK_NAMES {
			config.values.Set("link_names", "1")
		}

		if params.UnfurlLinks != DEFAULT_MESSAGE_UNFURL_LINKS {
			config.values.Set("unfurl_links", "true")
		}

		// I want to send a message with explicit `as_user` `true` and `unfurl_links` `false` in request.
		// Because setting `as_user` to `true` will change the default value for `unfurl_links` to `true` on Slack API side.
		if params.AsUser != DEFAULT_MESSAGE_ASUSER && params.UnfurlLinks == DEFAULT_MESSAGE_UNFURL_LINKS {
			config.values.Set("unfurl_links", "false")
		}
		if params.UnfurlMedia != DEFAULT_MESSAGE_UNFURL_MEDIA {
			config.values.Set("unfurl_media", "false")
		}
		if params.IconURL != DEFAULT_MESSAGE_ICON_URL {
			config.values.Set("icon_url", params.IconURL)
		}
		if params.IconEmoji != DEFAULT_MESSAGE_ICON_EMOJI {
			config.values.Set("icon_emoji", params.IconEmoji)
		}
		if params.Markdown != DEFAULT_MESSAGE_MARKDOWN {
			config.values.Set("mrkdwn", "false")
		}

		if params.ThreadTimestamp != DEFAULT_MESSAGE_THREAD_TIMESTAMP {
			config.values.Set("thread_ts", params.ThreadTimestamp)
		}
		if params.ReplyBroadcast != DEFAULT_MESSAGE_REPLY_BROADCAST {
			config.values.Set("reply_broadcast", "true")
		}

		return nil
	}
}

// TODO: this shouldn't exist anymore, ephemeral messages need to be updated to use the message sender.
func chatRequest(ctx context.Context, httpclient HTTPRequester, path string, values url.Values, debug bool) (*chatResponseFull, error) {
	response := &chatResponseFull{}

	err := postForm(ctx, httpclient, path, values, response, debug)
	if err != nil {
		return nil, err
	}
	if !response.Ok {
		return nil, errors.New(response.Error)
	}
	return response, nil
}
