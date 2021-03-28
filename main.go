// This Dead Man's Switch project has to run on a server with consistent
// uptime, it should be running this program in the background. It will
// send an email, then it expects an HTTP GET request with a token on
// port 9999 on the server. If that request won't arrive in a configured
// time span, the program will send the stored secret key to all the
// recipients that you specified.
// https://en.wikipedia.org/wiki/Dead_man%27s_switch.
// This project is licensed under GPLv3 and v.casalino@protonmail.com is
// the original author. Feel free to contribute, redistribute, repackage
// the software and whatever, just mention me when you do ;)
package main

// My resources:
// https://ieftimov.com/post/four-steps-daemonize-your-golang-programs/
// https://www.golangprograms.com/how-to-play-and-pause-execution-of-goroutine.html

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// ClockTick is the refresh tick for the timer of the switch
const DefaultClockTick = 24 * time.Hour

// The first 6 parameters are command line arguments and are
// better documented later. Later on you'll find internal values
// and variables to make the timer work.
type config struct {
	UserEmail   string
	MXServer    string
	MXPort      string
	Recipients  string
	Intervals   int
	Forgive     int
	ForgiveCode string
	Password    string
	Secret      string
	Tick        time.Duration
}

// checks will check the sanity of the parameters passed to the
// program. AKA the first fields in the config struct.
func (c *config) checks() error {

	var target = fmt.Sprint(c.MXServer, ":", c.MXPort)

	// Test if the host is reachable and the port is accessible
	// with a TCP connection.
	timeout := time.Duration(5) * time.Second

	if _, err := net.DialTimeout("tcp", target, timeout); err != nil {
		return err
	}

	// Parse sender and recipient email addresses as RFC 5322
	// compliant addresses thanks to this package
	// https://golang.org/pkg/net/mail/
	var addressParser mail.AddressParser

	if _, err := addressParser.Parse(cfg.UserEmail); err != nil {
		return err
	}
	if _, err := addressParser.ParseList(cfg.Recipients); err != nil {
		return err
	}

	// Sending a test mail to ensure the correct credentials
	// and move on. You don't want to spend the first interval
	// of the time wondering if you used the correct credentials
	// or you mistyped something.
	// https://golang.org/pkg/net/smtp/#PlainAuth
	auth := smtp.PlainAuth("", c.UserEmail, c.Password, target)
	testMsg := []byte("Test to check your credentials. Have a nice day :)")

	if err := smtp.SendMail(target, auth, c.UserEmail, []string{c.UserEmail}, testMsg); err != nil {
		return err
	}

	// Set the clock tick from arguments, default is 1 day
	if cfg.Intervals > 0 {
		cfg.Tick = time.Duration(cfg.Intervals * 24 * int(time.Hour))
	} else {
		cfg.Tick = DefaultClockTick
	}

	return nil
}

// getSecret acquires the secret from user input, reading until
// an EOF string is provided: "EOF<enter>"
// https://stackoverflow.com/a/30827547
func (c *config) getSecret() error {
	var lines []string

	// Acquire input until EOF string is passed in the
	// terminal input. This is not optimal, but useful
	fmt.Println("Enter Lines, reading until ^EOF<enter>:")
	scn := bufio.NewScanner(os.Stdin)
	for scn.Scan() {
		line := scn.Text()
		if len(line) == 1 {
			if line[0:3] == "EOF" {
				break
			}
		}
		lines = append(lines, line)
	}
	fmt.Println("Secret saved!")

	// Join the strings to get a message body to send.
	// Stores it in the config struct.
	c.Secret = strings.Join(lines, "\n")

	return nil
}

var cfg config

func banner() {
	fmt.Fprintf(os.Stdout, "            __              \n")
	fmt.Fprintf(os.Stdout, "       ____/ /___ ___  _____\n")
	fmt.Fprintf(os.Stdout, "      / __  / __ `__ \\/ ___/\n")
	fmt.Fprintf(os.Stdout, "     / /_/ / / / / / (__  ) \n")
	fmt.Fprintf(os.Stdout, "     \\__,_/_/ /_/ /_/____/  \n\n")
	fmt.Fprintf(os.Stdout, "-by 5amu (https://github.com/5amu)\n\n")
}

func flagParse() {
	// This email has to be an email on which you have access
	// with credentials, as they'll be requested by this program
	// during runtime. Practically speaking, this isn't meant to be a
	// permanent solution. Better implementation are welcome as contributions
	flag.StringVar(&cfg.UserEmail, "email", "", "Email of the owner")
	flag.StringVar(&cfg.Password, "password", "", "One Time Password for Email sending")

	// mxServer:mxPort should be the SMTP ports to the service you want to
	// use to send your emails, eg. Gmail, Outlook or a custom MX service.
	flag.StringVar(&cfg.MXServer, "mxserv", "", "Mail Server for sending emails")
	flag.StringVar(&cfg.MXPort, "mxport", "465", "Port for email sending")

	// recipients are the ones that you want to deliver your secret to.
	// those are the email addresses to whom your secret will be sent
	// if your switch will be triggered. Choose carefully.
	flag.StringVar(&cfg.Recipients, "recipients", "", "Comma-separated list of recipients")

	// intervals is the interval, expressed in days, in which an email will be sent
	// from the email address you provided to itself, enstablishing the switch, and
	// forgive is the number of times the kill switch will forgive the owner if it
	// fails to notify its "aliveness".
	flag.IntVar(&cfg.Intervals, "interval", 0, "Interval (days) for the switch")
	flag.IntVar(&cfg.Forgive, "forgive", 1, "Tries before actually sending emails")

	flag.Usage = func() {
		fmt.Fprint(os.Stdout, "Activate a Dead Man's Switch. Your reason, your business :)\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
}

// generateCode will generate the alphanumeric code that has to be supplied
// to the http endpoint to increase the Forgive counter
func generateCode(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

// clock is the effective clock of the program. Its purpose is to
// react to time changes by triggering the switch when time passes
// and the target isn't "alive".
func clock(ctx context.Context, cfg *config) error {
	for {
		select {
		// When the context is killed, or done, this will just
		// exit the function with no errors
		case <-ctx.Done():
			return nil
		// This will be triggered at every ClockTick
		case <-time.Tick(cfg.Tick):
			cfg.Forgive -= 1
			// If the user is not answering for whatever reason after the
			// n times defined by Forgive, then trigger the dead man switch
			if cfg.Forgive < 0 {
				// Send email to each wmail in recipients variable
				auth := smtp.PlainAuth("", cfg.UserEmail, cfg.Password, cfg.MXServer)
				msg := []byte(cfg.UserEmail + "'s Dead Man's Switch here, the secret is" + cfg.Secret)
				if err := smtp.SendMail(cfg.MXServer+":"+cfg.MXPort, auth, cfg.UserEmail, strings.Split(cfg.Recipients, ","), msg); err != nil {
					return err
				}
				return nil
			}
			// If the user has more tries, then generate a new code and send it to
			// its own mailbox with the code to use
			cfg.ForgiveCode = generateCode(16)
			auth := smtp.PlainAuth("", cfg.UserEmail, cfg.Password, cfg.MXServer)
			msg := []byte("Your Dead Man's Switch here, are you still there? Make a request: http://server:9999/" + cfg.ForgiveCode)
			if err := smtp.SendMail(cfg.MXServer+":"+cfg.MXPort, auth, cfg.UserEmail, []string{cfg.UserEmail}, msg); err != nil {
				return err
			}
		}
	}
}

// waitForCode will start a http server, waiting for the code as GET
// parameter, if it receives it, then increases the Forgive value, so that
// the time limit increases
func waitForCode() error {

	http.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if cfg.ForgiveCode != "" && r.RequestURI == cfg.ForgiveCode {
			cfg.Forgive += 1
			cfg.ForgiveCode = ""
		}
	})

	if err := http.ListenAndServe(":9999", nil); err != nil {
		return err
	}

	return nil
}

func main() {

	// Parse flags, it is not optimal, but works... eg. It will
	// print an usage only if the flah -h or -help is specified
	flagParse()

	// This banner should be colored in the future, for now,
	// let's just make this program work without hiccups
	banner()

	// This will make sure that all arguments are present and
	// correctly passed to the program. Will also check the
	// connections to the Mail eXchange Server
	if err := cfg.checks(); err != nil {
		panic(err)
	}

	// This reads the secret from stdin and stores it in the
	// config struct as message body for the dead man switch
	if err := cfg.getSecret(); err != nil {
		panic(err)
	}

	// Defining a context for aborting execution gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Starting the HTTP server as go routine. This won't stop
	// execution of the next instructions
	go func() {
		if err := waitForCode(); err != nil {
			panic(err)
		}
	}()

	// This section enstablishes a context and starts the clock and
	// panics if an error is returned.
	// https://ieftimov.com/post/four-steps-daemonize-your-golang-programs/
	if err := clock(ctx, &config{}); err != nil {
		panic(err)
	}
}
