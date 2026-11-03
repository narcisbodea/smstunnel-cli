package cli

const exitCodesHelp = `Exit codes:
  0  done (a text not confirmed by the phone counts as sent: don't send it again)
  1  something unexpected
  2  invalid arguments, or SMSTunnel refused the input (400; 422: --idempotency-key used for another text)
  3  no API key, or SMSTunnel refused it (401)
  4  the key or your plan doesn't allow it (403)
  5  not found (404)
  6  too many requests, or today's allowance is used up (429); 409: --idempotency-key still in progress
  7  SMSTunnel could not be reached or had a problem (5xx)
  8  the text failed (send --wait)
  9  still waiting for your phone when --wait gave up
130  stopped waiting with Ctrl+C (--wait); Ctrl+C ends received --watch with 0`

func mainHelp() string {
	return `smstunnel ` + Version + `: send texts from your computer through your own Android phone and number.

Usage: smstunnel <command> [options]

Commands:
  login                       Save your API key (from ` + dashboardURL + ` → Apps → Add an app)
  logout                      Remove the saved key from this computer
  phones                      List your phones and which are online
  send <number> <message…>    Hand a text to your phone, which sends it from your number
  code <number> <message…>    Send a sign-in code (the text must contain the code)
  status <message-id>         Where a text is now
  received                    Texts your phone received (needs a key allowed to read them)
  usage                       Your plan, today's usage and what is left

Options for every command:
  --json                Print JSON (errors go to stderr, as JSON too)
  --api-base <url>      API address (default ` + defaultAPIBase + `)
  -h, --help            Help for a command, like: smstunnel send --help
  -v, --version         Print the version

Environment:
  SMSTUNNEL_API_KEY     A key to use instead of the saved one
  SMSTUNNEL_API_BASE    The API address, like --api-base

One number per text: each text goes to one recipient.

` + exitCodesHelp
}

var commandHelp = map[string]string{
	"login": `Usage: smstunnel login

Asks for your API key (hidden while you paste it), checks it with SMSTunnel and saves it:
in your macOS Keychain, Windows Credential Manager or Linux keyring (Secret Service) when there
is one, otherwise in a file only your user account can read.

Get a key in your dashboard: ` + dashboardURL + ` → Apps → Add an app. It is shown only once.
To pipe it in instead: echo "$SMSTUNNEL_KEY" | smstunnel login`,

	"logout": `Usage: smstunnel logout

Removes the saved key from this computer. The key keeps working until you deactivate or
delete it in your dashboard (Apps).`,

	"phones": `Usage: smstunnel phones [--json]

Lists the phones linked to your account, which are online, and their ids for --phone.`,

	"send": `Usage: smstunnel send <number> <message…> [options]

Hands a text to your phone, which sends it from your number. One number per text.
With no message, or "-", the text is read from standard input.

Options:
  --phone <id or name>  Which of your phones sends it (default: your default phone)
  --country <code>      Country code for a number written without + or 00, like +40
  --wait                Wait until your phone sent it (or it failed)
  --timeout <seconds>   How long --wait waits (default 120)
  --idempotency-key <key>
                        Run the same command again with the same key (within 24 hours) and
                        SMSTunnel gives back the first answer instead of sending a second text

Examples:
  smstunnel send +40712345678 "Your order is ready for pickup"
  smstunnel send 0712345678 --country +40 --phone "Store phone" --wait "See you at 10"
  echo "Backup finished" | smstunnel send +40712345678
  smstunnel send +40712345678 "Order 1042 is ready" --idempotency-key order-1042-ready

Texts go out in the order you send them, up to 1600 characters each. If none of your phones is
online, the text waits and goes out when one is back online (within 3 days).`,

	"code": `Usage: smstunnel code <number> <message…> [options]

Sends a sign-in code you generated, from your own phone. The text must contain the code:
4 to 10 digits on their own (482913) or two groups of 3 joined by a dash (482-913), up to
320 characters. Codes have their own daily allowance.

Options: --phone, --country, --wait, --timeout and --idempotency-key, as for "smstunnel send".

Example:
  smstunnel code +40712345678 "Your sign-in code is 482913. It expires in 5 minutes."`,

	"status": `Usage: smstunnel status <message-id> [--json]

Where a text is now: waiting for your phone, sent, or not sent (and why).`,

	"received": `Usage: smstunnel received [options]

Texts your phone received after you turned on "Sync received texts" in the SMSTunnel app.
The key must be allowed to read them: in your dashboard, Apps → "Allow reading received texts"
(or "Can also read received texts" when you add the app).

Options:
  --since <when>        A date (2026-11-03, 2026-11-03T08:00:00Z) or a span back from now (30m, 2h, 1d)
  --from <number>       Only texts from this number
  --phone <id or name>  Only texts one of your phones received
  --limit <n>           How many, 1 to 100 (default 20)
  --watch               Keep printing new texts as they arrive (Ctrl+C stops)
  --interval <seconds>  How often --watch checks (default 10, at least 3)`,

	"usage": `Usage: smstunnel usage [--json]

Your plan, how many texts and sign-in codes you used today and how many are left.`,
}

// helpFor is the help of a command, or the main help when the command is missing or unknown.
func helpFor(command string) string {
	if text, ok := commandHelp[command]; ok {
		return text
	}
	return mainHelp()
}
