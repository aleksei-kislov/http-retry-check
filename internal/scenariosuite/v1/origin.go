package scenariosuite

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	maxRequestHeaderSize = 64 << 10
	maxRequestBodySize   = 1 << 20
	trailingProbeTimeout = 5 * time.Millisecond
)

type endpointRole uint8

const (
	sourceEndpoint endpointRole = iota + 1
	redirectEndpoint
)

type scenarioEndpoint struct {
	listener net.Listener
	address  netip.AddrPort
	role     endpointRole
	done     chan struct{}
}

type capturedAttempt struct {
	role                  endpointRole
	headersObserved       bool
	complete              bool
	methodConsistent      bool
	destinationConsistent bool
	bodyConsistent        bool
	credentialExact       bool
	credentialExposed     bool
}

type scenarioOrigin struct {
	scenario          ScenarioID
	endpoints         []*scenarioEndpoint
	connectionTimeout time.Duration
	delay             func(context.Context) bool
	stopCase          context.CancelFunc

	wait                  sync.WaitGroup
	closeOnce             sync.Once
	mu                    sync.Mutex
	closing               bool
	connections           map[net.Conn]bool
	attempts              []capturedAttempt
	active                uint32
	overlapCount          uint32
	effectCount           uint64
	retryAfterEffect      uint32
	retryAfterUnconfirmed uint32
	retryBeforeResponse   uint32
	pendingResponse       uint32
	responseTry           uint32
	responseDone          uint32
	firstResponse         bool
	delayDone             uint32
	captureMissing        bool
	serveFailed           bool
	closeFailed           bool
}

func newScenarioOrigin(
	scenario ScenarioID,
	listeners []net.Listener,
	connectionTimeout time.Duration,
	delay func(context.Context) bool,
	stopCase context.CancelFunc,
) (*scenarioOrigin, bool) {
	wantListeners := 1
	if scenario == ScenarioCrossOriginRedirectCredentials {
		wantListeners = 2
	}
	if !knownScenario(scenario) || len(listeners) != wantListeners || connectionTimeout <= 0 ||
		delay == nil || stopCase == nil {
		return nil, false
	}
	endpoints := make([]*scenarioEndpoint, 0, len(listeners))
	seen := make(map[netip.AddrPort]struct{}, len(listeners))
	for index, listener := range listeners {
		address, valid := listenerAddress(listener)
		if !valid {
			return nil, false
		}
		if _, duplicate := seen[address]; duplicate {
			return nil, false
		}
		seen[address] = struct{}{}
		role := sourceEndpoint
		if index == 1 {
			role = redirectEndpoint
		}
		endpoints = append(endpoints, &scenarioEndpoint{
			listener: listener, address: address, role: role, done: make(chan struct{}),
		})
	}
	return &scenarioOrigin{
		scenario: scenario, endpoints: endpoints, connectionTimeout: connectionTimeout,
		delay: delay, stopCase: stopCase, connections: make(map[net.Conn]bool),
		attempts: make([]capturedAttempt, 0, maxObservedAttempts),
	}, true
}

func (origin *scenarioOrigin) sourceTarget() string {
	return endpointTarget(origin.endpoints[0].address)
}

func (origin *scenarioOrigin) redirectTarget() string {
	if len(origin.endpoints) != 2 {
		return ""
	}
	return endpointTarget(origin.endpoints[1].address)
}

func endpointTarget(address netip.AddrPort) string {
	return "http://" + address.String() + controlledPath
}

func (origin *scenarioOrigin) start(ctx context.Context) {
	for _, endpoint := range origin.endpoints {
		go origin.serve(ctx, endpoint)
	}
}

func (origin *scenarioOrigin) serve(ctx context.Context, endpoint *scenarioEndpoint) {
	defer close(endpoint.done)
	for {
		connection, err := endpoint.listener.Accept()
		if err != nil {
			origin.mu.Lock()
			if !origin.closing && ctx.Err() == nil {
				origin.serveFailed = true
				origin.captureMissing = true
			}
			origin.mu.Unlock()
			return
		}
		index, admitted := origin.admit(connection, endpoint.role)
		if !admitted {
			origin.recordConnectionClose(connection.Close())
			origin.stopCase()
			origin.closeAdmissions()
			return
		}
		go origin.observe(ctx, connection, endpoint, index)
	}
}

func (origin *scenarioOrigin) admit(connection net.Conn, role endpointRole) (int, bool) {
	origin.mu.Lock()
	defer origin.mu.Unlock()
	if origin.closing {
		return 0, false
	}
	if len(origin.attempts) >= maxObservedAttempts {
		origin.captureMissing = true
		return 0, false
	}
	if origin.active != 0 {
		origin.overlapCount++
		origin.captureMissing = true
	}
	if len(origin.attempts) != 0 {
		if (origin.scenario == ScenarioAcceptThenDisconnect ||
			origin.scenario == ScenarioChangedBodyRetry ||
			origin.scenario == ScenarioDelayedResponse) && origin.effectCount != 0 {
			origin.retryAfterEffect++
		}
		if origin.scenario == ScenarioDisconnectBeforeAcceptance &&
			origin.effectCount == 0 && origin.hasCompleteAttempt() {
			origin.retryAfterUnconfirmed++
		}
		if origin.scenario == ScenarioDelayedResponse && origin.pendingResponse != 0 {
			origin.retryBeforeResponse++
		}
	}
	origin.attempts = append(origin.attempts, capturedAttempt{
		role: role, methodConsistent: true, destinationConsistent: true, bodyConsistent: true,
	})
	origin.connections[connection] = true
	origin.active++
	origin.wait.Add(1)
	return len(origin.attempts) - 1, true
}

func (origin *scenarioOrigin) hasCompleteAttempt() bool {
	for _, attempt := range origin.attempts {
		if attempt.complete {
			return true
		}
	}
	return false
}

func (origin *scenarioOrigin) observe(
	ctx context.Context,
	connection net.Conn,
	endpoint *scenarioEndpoint,
	index int,
) {
	activeReleased := false
	pendingResponse := false
	defer origin.wait.Done()
	defer func() {
		origin.recordConnectionClose(connection.Close())
		origin.mu.Lock()
		delete(origin.connections, connection)
		if !activeReleased && origin.active != 0 {
			origin.active--
		}
		origin.mu.Unlock()
	}()
	defer func() {
		if pendingResponse {
			origin.finishPendingResponse()
			activeReleased = true
		}
	}()

	connectionDeadline := time.Now().Add(origin.connectionTimeout)
	if connection.SetDeadline(connectionDeadline) != nil {
		origin.markCaptureIncomplete()
		return
	}
	observed := readScenarioAttempt(connection, endpoint.address, connectionDeadline)
	origin.mu.Lock()
	origin.attempts[index] = capturedAttempt{
		role: endpoint.role, headersObserved: observed.headersObserved, complete: observed.complete,
		methodConsistent:      observed.methodConsistent,
		destinationConsistent: observed.destinationConsistent,
		bodyConsistent:        observed.bodyConsistent,
		credentialExact:       observed.credentialExact,
		credentialExposed:     observed.credentialExposed,
	}
	if !observed.captureComplete {
		origin.captureMissing = true
	}
	if observed.complete {
		origin.connections[connection] = false
		if origin.commitsEffect(endpoint.role) {
			origin.effectCount++
			if origin.scenario == ScenarioDelayedResponse {
				origin.pendingResponse++
				pendingResponse = true
			}
		}
	}
	origin.mu.Unlock()
	if !observed.complete {
		return
	}
	if origin.scenario != ScenarioDelayedResponse {
		origin.releaseActive()
		activeReleased = true
	}

	switch origin.scenario {
	case ScenarioAcceptThenDisconnect, ScenarioDisconnectBeforeAcceptance, ScenarioChangedBodyRetry:
		return
	case ScenarioCrossOriginRedirectCredentials:
		if endpoint.role == sourceEndpoint {
			origin.writeResponse(
				connection, index, endpoint.role, redirectResponse(origin.redirectTarget()),
			)
		} else {
			origin.writeResponse(connection, index, endpoint.role, []byte(noContentResponseText))
		}
		origin.recordPostResponseProbe(
			connection, observed.remainingReadBudget, connectionDeadline,
		)
	case ScenarioRetryLimit:
		origin.writeResponse(connection, index, endpoint.role, []byte(unavailableResponseText))
		origin.recordPostResponseProbe(
			connection, observed.remainingReadBudget, connectionDeadline,
		)
	case ScenarioDelayedResponse:
		monitor := startTrailingMonitor(
			connection, connectionDeadline, observed.remainingReadBudget, &origin.wait,
		)
		if !origin.delay(ctx) {
			_, _, closeVerified := monitor.finish(ctx, connection, 0)
			if !closeVerified {
				origin.markCloseFailed()
			}
			origin.markCaptureIncomplete()
			return
		}
		origin.mu.Lock()
		origin.delayDone++
		origin.mu.Unlock()
		origin.writeDelayedResponse(connection, index, []byte(noContentResponseText))
		pendingResponse = false
		activeReleased = true
		trailing, complete, closeVerified := monitor.finish(
			ctx, connection, trailingProbeTimeout,
		)
		if !closeVerified {
			origin.markCloseFailed()
		}
		if trailing || !complete {
			origin.markCaptureIncomplete()
		}
	}
}

func (origin *scenarioOrigin) finishPendingResponse() {
	origin.mu.Lock()
	defer origin.mu.Unlock()
	if origin.pendingResponse != 0 {
		origin.pendingResponse--
	}
	if origin.active != 0 {
		origin.active--
	}
}

func (origin *scenarioOrigin) recordPostResponseProbe(
	connection net.Conn,
	remainingBudget int64,
	connectionDeadline time.Time,
) {
	trailing, complete := probeConnectionTrailing(
		connection, remainingBudget, connectionDeadline,
	)
	if trailing || !complete {
		origin.markCaptureIncomplete()
	}
}

func (origin *scenarioOrigin) releaseActive() {
	origin.mu.Lock()
	defer origin.mu.Unlock()
	if origin.active != 0 {
		origin.active--
	}
}

func (origin *scenarioOrigin) commitsEffect(role endpointRole) bool {
	switch origin.scenario {
	case ScenarioAcceptThenDisconnect, ScenarioChangedBodyRetry, ScenarioDelayedResponse:
		return true
	case ScenarioCrossOriginRedirectCredentials:
		return role == redirectEndpoint
	default:
		return false
	}
}

const (
	noContentResponseText   = "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
	unavailableResponseText = "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
)

func redirectResponse(target string) []byte {
	response := make([]byte, 0, 96+len(target))
	response = append(response, "HTTP/1.1 307 Temporary Redirect\r\nLocation: "...)
	response = append(response, target...)
	response = append(response, "\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"...)
	return response
}

func (origin *scenarioOrigin) writeResponse(
	connection net.Conn,
	index int,
	role endpointRole,
	response []byte,
) {
	origin.mu.Lock()
	origin.responseTry++
	origin.mu.Unlock()
	complete := writeAll(connection, response)
	clear(response)
	origin.mu.Lock()
	if complete {
		origin.responseDone++
		if index == 0 && (origin.scenario != ScenarioCrossOriginRedirectCredentials || role == sourceEndpoint) {
			origin.firstResponse = true
		}
	}
	origin.mu.Unlock()
}

func (origin *scenarioOrigin) writeDelayedResponse(connection net.Conn, index int, response []byte) {
	origin.mu.Lock()
	origin.responseTry++
	origin.mu.Unlock()
	complete := writeAll(connection, response)
	clear(response)
	origin.mu.Lock()
	if complete {
		origin.responseDone++
		if index == 0 {
			origin.firstResponse = true
		}
	}
	if origin.pendingResponse != 0 {
		origin.pendingResponse--
	}
	if origin.active != 0 {
		origin.active--
	}
	origin.mu.Unlock()
}

func writeAll(writer io.Writer, value []byte) bool {
	for len(value) != 0 {
		count, err := writer.Write(value)
		if count > 0 {
			value = value[count:]
		}
		if err != nil || count == 0 {
			return false
		}
	}
	return true
}

type readObservation struct {
	headersObserved       bool
	complete              bool
	captureComplete       bool
	methodConsistent      bool
	destinationConsistent bool
	bodyConsistent        bool
	credentialExact       bool
	credentialExposed     bool
	remainingReadBudget   int64
}

func readScenarioAttempt(
	connection net.Conn,
	address netip.AddrPort,
	connectionDeadline time.Time,
) readObservation {
	result := readObservation{
		methodConsistent: true, destinationConsistent: true, bodyConsistent: true,
	}
	header, err := readBoundedHeader(connection)
	result.credentialExposed = bytes.Contains(header, []byte(syntheticCredentialMarker))
	if err != nil {
		clear(header)
		return result
	}
	parsed, valid := parseRequestHead(header, address)
	clear(header)
	if !valid {
		return result
	}
	result.headersObserved = true
	result.methodConsistent = parsed.methodConsistent
	result.destinationConsistent = parsed.destinationConsistent
	result.credentialExact = parsed.credentialExact
	if !parsed.protocolExact {
		return result
	}
	wire := newBoundedWireReader(connection, maxRequestBodySize+1)
	defer wire.clear()
	body := readRequestBody(wire, parsed)
	result.bodyConsistent = body.consistent
	if !body.complete {
		result.remainingReadBudget = wire.remainingBudget()
		return result
	}
	result.complete = true
	trailing, probeComplete := probeTrailingInput(wire, connection, connectionDeadline)
	result.remainingReadBudget = wire.remainingBudget()
	result.captureComplete = !trailing && probeComplete && result.remainingReadBudget != 0
	return result
}

func readBoundedHeader(reader io.Reader) ([]byte, error) {
	header := make([]byte, 0, 4096)
	var current [1]byte
	for len(header) < maxRequestHeaderSize {
		if _, err := io.ReadFull(reader, current[:]); err != nil {
			return header, err
		}
		header = append(header, current[0])
		length := len(header)
		if length >= 4 && bytes.Equal(header[length-4:], []byte("\r\n\r\n")) {
			return header, nil
		}
	}
	return header, io.ErrUnexpectedEOF
}

func probeTrailingInput(
	reader *boundedWireReader,
	connection net.Conn,
	connectionDeadline time.Time,
) (bool, bool) {
	if reader == nil || connection == nil || connectionDeadline.IsZero() {
		return false, false
	}
	if reader.bufferedCount() != 0 {
		return true, true
	}
	if reader.remainingBudget() == 0 {
		return false, false
	}
	probeDeadline, fullWindow := boundedPhaseDeadline(connectionDeadline, trailingProbeTimeout)
	if connection.SetReadDeadline(probeDeadline) != nil {
		return false, false
	}
	_, err := reader.readByte()
	if err == nil {
		return true, true
	}
	if errors.Is(err, io.EOF) {
		return false, true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return false, fullWindow
	}
	return false, false
}

type parsedRequestHead struct {
	methodConsistent      bool
	destinationConsistent bool
	protocolExact         bool
	credentialExact       bool
	contentLength         int64
	chunked               bool
}

func parseRequestHead(header []byte, address netip.AddrPort) (parsedRequestHead, bool) {
	result := parsedRequestHead{}
	if len(header) < 4 || !bytes.HasSuffix(header, []byte("\r\n\r\n")) {
		return result, false
	}
	lines := bytes.Split(header, []byte("\r\n"))
	if len(lines) < 3 || len(lines[len(lines)-1]) != 0 || len(lines[len(lines)-2]) != 0 {
		return result, false
	}
	requestLine := lines[0]
	firstSpace := bytes.IndexByte(requestLine, ' ')
	if firstSpace <= 0 {
		return result, false
	}
	secondRelative := bytes.IndexByte(requestLine[firstSpace+1:], ' ')
	if secondRelative <= 0 {
		return result, false
	}
	secondSpace := firstSpace + 1 + secondRelative
	if bytes.IndexByte(requestLine[secondSpace+1:], ' ') >= 0 {
		return result, false
	}
	method := requestLine[:firstSpace]
	target := requestLine[firstSpace+1 : secondSpace]
	protocol := requestLine[secondSpace+1:]
	if !headerToken(method) || !visibleRequestTarget(target) || !httpProtocol(protocol) {
		return result, false
	}

	hostCount := 0
	var host []byte
	authorizationCount := 0
	authorizationExact := false
	contentLengthSeen := false
	transferEncodingSeen := false
	for _, line := range lines[1 : len(lines)-2] {
		colon := bytes.IndexByte(line, ':')
		if colon <= 0 || !headerToken(line[:colon]) {
			return parsedRequestHead{}, false
		}
		name := line[:colon]
		value := trimOptionalWhitespace(line[colon+1:])
		if !headerFieldValue(value) {
			return parsedRequestHead{}, false
		}
		switch {
		case asciiEqualFold(name, []byte("Host")):
			hostCount++
			host = value
		case asciiEqualFold(name, []byte("Authorization")):
			authorizationCount++
			authorizationExact = bytes.Equal(value, []byte(syntheticCredential))
		case asciiEqualFold(name, []byte("Content-Length")):
			if contentLengthSeen {
				return parsedRequestHead{}, false
			}
			length, ok := parseContentLength(value)
			if !ok {
				return parsedRequestHead{}, false
			}
			contentLengthSeen = true
			result.contentLength = length
		case asciiEqualFold(name, []byte("Transfer-Encoding")):
			if transferEncodingSeen || !asciiEqualFold(value, []byte("chunked")) {
				return parsedRequestHead{}, false
			}
			transferEncodingSeen = true
			result.chunked = true
		}
	}
	if hostCount != 1 || contentLengthSeen && transferEncodingSeen {
		return parsedRequestHead{}, false
	}
	result.methodConsistent = bytes.Equal(method, []byte("POST"))
	result.destinationConsistent = bytes.Equal(target, []byte(controlledPath)) &&
		bytes.Equal(host, []byte(address.String()))
	result.protocolExact = bytes.Equal(protocol, []byte("HTTP/1.1"))
	result.credentialExact = authorizationCount == 1 && authorizationExact
	return result, true
}

func visibleRequestTarget(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	for _, current := range value {
		if current < 0x21 || current > 0x7e {
			return false
		}
	}
	return true
}

func httpProtocol(value []byte) bool {
	return len(value) == 8 && bytes.Equal(value[:5], []byte("HTTP/")) &&
		value[5] >= '0' && value[5] <= '9' && value[6] == '.' &&
		value[7] >= '0' && value[7] <= '9'
}

func headerToken(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	for _, current := range value {
		if !headerTokenByte(current) {
			return false
		}
	}
	return true
}

func headerTokenByte(current byte) bool {
	if current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' ||
		current >= '0' && current <= '9' {
		return true
	}
	switch current {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

func headerFieldValue(value []byte) bool {
	for _, current := range value {
		if current != '\t' && (current < 0x20 || current == 0x7f) {
			return false
		}
	}
	return true
}

func asciiEqualFold(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index, current := range left {
		if current >= 'A' && current <= 'Z' {
			current += 'a' - 'A'
		}
		expected := right[index]
		if expected >= 'A' && expected <= 'Z' {
			expected += 'a' - 'A'
		}
		if current != expected {
			return false
		}
	}
	return true
}

func trimOptionalWhitespace(value []byte) []byte {
	for len(value) != 0 && (value[0] == ' ' || value[0] == '\t') {
		value = value[1:]
	}
	for len(value) != 0 && (value[len(value)-1] == ' ' || value[len(value)-1] == '\t') {
		value = value[:len(value)-1]
	}
	return value
}

func parseContentLength(value []byte) (int64, bool) {
	if len(value) == 0 {
		return 0, false
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	var result int64
	for _, current := range value {
		if current < '0' || current > '9' {
			return 0, false
		}
		digit := int64(current - '0')
		if result > (maxInt64-digit)/10 {
			return 0, false
		}
		result = result*10 + digit
	}
	return result, true
}

type wireFailure uint8

const wireBudgetExhausted wireFailure = 1

func (wireFailure) Error() string {
	return "HTTP request wire budget exhausted"
}

type boundedWireReader struct {
	reader    io.Reader
	buffer    [4096]byte
	offset    int
	count     int
	remaining int64
}

func newBoundedWireReader(reader io.Reader, budget int64) *boundedWireReader {
	return &boundedWireReader{reader: reader, remaining: budget}
}

func (reader *boundedWireReader) readByte() (byte, error) {
	if reader == nil || reader.reader == nil {
		return 0, io.ErrUnexpectedEOF
	}
	if reader.offset != reader.count {
		result := reader.buffer[reader.offset]
		reader.offset++
		return result, nil
	}
	if reader.remaining == 0 {
		return 0, wireBudgetExhausted
	}
	requested := int64(len(reader.buffer))
	if reader.remaining < requested {
		requested = reader.remaining
	}
	count, err := reader.reader.Read(reader.buffer[:requested])
	if count > 0 {
		reader.remaining -= int64(count)
		reader.offset = 1
		reader.count = count
		return reader.buffer[0], nil
	}
	if err != nil {
		return 0, err
	}
	return 0, io.ErrNoProgress
}

func (reader *boundedWireReader) bufferedCount() int {
	if reader == nil {
		return 0
	}
	return reader.count - reader.offset
}

func (reader *boundedWireReader) remainingBudget() int64 {
	if reader == nil {
		return 0
	}
	return reader.remaining
}

func (reader *boundedWireReader) clear() {
	if reader == nil {
		return
	}
	clear(reader.buffer[:])
}

type capturedBody struct {
	complete   bool
	consistent bool
}

type bodyAccumulator struct {
	count      int
	consistent bool
}

func newBodyAccumulator() bodyAccumulator {
	return bodyAccumulator{consistent: true}
}

func (body *bodyAccumulator) add(value byte) bool {
	if body.count >= maxRequestBodySize {
		body.count++
		body.consistent = false
		return false
	}
	if body.count >= len(syntheticBodyText) || syntheticBodyText[body.count] != value {
		body.consistent = false
	}
	body.count++
	return true
}

func (body bodyAccumulator) completed() capturedBody {
	return capturedBody{complete: true, consistent: body.consistent && body.count == len(syntheticBodyText)}
}

func (body bodyAccumulator) failed() capturedBody {
	return capturedBody{consistent: body.consistent}
}

func readRequestBody(reader *boundedWireReader, parsed parsedRequestHead) capturedBody {
	body := newBodyAccumulator()
	if parsed.chunked {
		return readChunkedBody(reader, &body)
	}
	for index := int64(0); index < parsed.contentLength; index++ {
		value, err := reader.readByte()
		if err != nil || !body.add(value) {
			return body.failed()
		}
	}
	return body.completed()
}

func readChunkedBody(reader *boundedWireReader, body *bodyAccumulator) capturedBody {
	for {
		size, valid := readChunkSize(reader)
		if !valid {
			return body.failed()
		}
		if size == 0 {
			if readTrailers(reader) {
				return body.completed()
			}
			return body.failed()
		}
		for index := uint64(0); index < size; index++ {
			value, err := reader.readByte()
			if err != nil || !body.add(value) {
				return body.failed()
			}
		}
		if !readCRLF(reader) {
			return body.failed()
		}
	}
}

func readChunkSize(reader *boundedWireReader) (uint64, bool) {
	var size uint64
	digits := 0
	for {
		value, err := reader.readByte()
		if err != nil {
			return 0, false
		}
		digit, valid := hexValue(value)
		if !valid {
			if digits == 0 {
				return 0, false
			}
			return size, readChunkExtensions(reader, value)
		}
		if size > (^uint64(0)-uint64(digit))/16 {
			return 0, false
		}
		size = size*16 + uint64(digit)
		digits++
	}
}

type chunkExtensionState uint8

const (
	chunkExtensionBeforeDelimiter chunkExtensionState = iota + 1
	chunkExtensionBeforeDelimiterWhitespace
	chunkExtensionNameStart
	chunkExtensionName
	chunkExtensionAfterNameWhitespace
	chunkExtensionValueStart
	chunkExtensionTokenValue
	chunkExtensionQuotedValue
	chunkExtensionQuotedPair
)

func readChunkExtensions(reader *boundedWireReader, current byte) bool {
	state := chunkExtensionBeforeDelimiter
	for {
		switch state {
		case chunkExtensionBeforeDelimiter:
			switch {
			case current == '\r':
				lineFeed, err := reader.readByte()
				return err == nil && lineFeed == '\n'
			case current == ';':
				state = chunkExtensionNameStart
			case chunkOptionalWhitespace(current):
				state = chunkExtensionBeforeDelimiterWhitespace
			default:
				return false
			}
		case chunkExtensionBeforeDelimiterWhitespace:
			switch {
			case chunkOptionalWhitespace(current):
			case current == ';':
				state = chunkExtensionNameStart
			default:
				return false
			}
		case chunkExtensionNameStart:
			switch {
			case chunkOptionalWhitespace(current):
			case headerTokenByte(current):
				state = chunkExtensionName
			default:
				return false
			}
		case chunkExtensionName:
			switch {
			case headerTokenByte(current):
			case current == '=':
				state = chunkExtensionValueStart
			case current == ';':
				state = chunkExtensionNameStart
			case current == '\r':
				lineFeed, err := reader.readByte()
				return err == nil && lineFeed == '\n'
			case chunkOptionalWhitespace(current):
				state = chunkExtensionAfterNameWhitespace
			default:
				return false
			}
		case chunkExtensionAfterNameWhitespace:
			switch {
			case chunkOptionalWhitespace(current):
			case current == '=':
				state = chunkExtensionValueStart
			case current == ';':
				state = chunkExtensionNameStart
			default:
				return false
			}
		case chunkExtensionValueStart:
			switch {
			case chunkOptionalWhitespace(current):
			case headerTokenByte(current):
				state = chunkExtensionTokenValue
			case current == '"':
				state = chunkExtensionQuotedValue
			default:
				return false
			}
		case chunkExtensionTokenValue:
			if !headerTokenByte(current) {
				state = chunkExtensionBeforeDelimiter
				continue
			}
		case chunkExtensionQuotedValue:
			switch {
			case current == '"':
				state = chunkExtensionBeforeDelimiter
			case current == '\\':
				state = chunkExtensionQuotedPair
			case !chunkQuotedTextByte(current):
				return false
			}
		case chunkExtensionQuotedPair:
			if !chunkQuotedPairByte(current) {
				return false
			}
			state = chunkExtensionQuotedValue
		default:
			return false
		}
		next, err := reader.readByte()
		if err != nil {
			return false
		}
		current = next
	}
}

func chunkOptionalWhitespace(value byte) bool {
	return value == ' ' || value == '\t'
}

func chunkQuotedTextByte(value byte) bool {
	return value == '\t' || value == ' ' || value == 0x21 ||
		value >= 0x23 && value <= 0x5b || value >= 0x5d && value <= 0x7e || value >= 0x80
}

func chunkQuotedPairByte(value byte) bool {
	return value == '\t' || value == ' ' || value >= 0x21 && value <= 0x7e || value >= 0x80
}

func readTrailers(reader *boundedWireReader) bool {
	lineLength := 0
	nameLength := 0
	colonSeen := false
	for {
		value, err := reader.readByte()
		if err != nil {
			return false
		}
		if value == '\r' {
			lineFeed, lineErr := reader.readByte()
			if lineErr != nil || lineFeed != '\n' {
				return false
			}
			if lineLength == 0 {
				return true
			}
			if !colonSeen || nameLength == 0 {
				return false
			}
			lineLength = 0
			nameLength = 0
			colonSeen = false
			continue
		}
		if value == '\n' || value != '\t' && (value < 0x20 || value == 0x7f) {
			return false
		}
		if !colonSeen {
			if value == ':' {
				if nameLength == 0 {
					return false
				}
				colonSeen = true
			} else if !headerTokenByte(value) {
				return false
			} else {
				nameLength++
			}
		}
		lineLength++
	}
}

func readCRLF(reader *boundedWireReader) bool {
	carriageReturn, firstErr := reader.readByte()
	if firstErr != nil || carriageReturn != '\r' {
		return false
	}
	lineFeed, secondErr := reader.readByte()
	return secondErr == nil && lineFeed == '\n'
}

func hexValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

type trailingMonitorResult struct {
	trailing bool
	complete bool
}

type trailingMonitor struct {
	stop chan struct{}
	done chan trailingMonitorResult
}

func startTrailingMonitor(
	connection net.Conn,
	connectionDeadline time.Time,
	remainingBudget int64,
	quiescence *sync.WaitGroup,
) *trailingMonitor {
	monitor := &trailingMonitor{
		stop: make(chan struct{}), done: make(chan trailingMonitorResult, 1),
	}
	if connection == nil || connectionDeadline.IsZero() || remainingBudget <= 0 || quiescence == nil ||
		connection.SetReadDeadline(connectionDeadline) != nil {
		monitor.done <- trailingMonitorResult{}
		return monitor
	}
	quiescence.Add(1)
	go func() {
		defer quiescence.Done()
		var current [1]byte
		count, err := connection.Read(current[:])
		if count != 0 {
			monitor.done <- trailingMonitorResult{trailing: true, complete: true}
			return
		}
		if errors.Is(err, io.EOF) {
			monitor.done <- trailingMonitorResult{complete: true}
			return
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			select {
			case <-monitor.stop:
				monitor.done <- trailingMonitorResult{complete: true}
			default:
				monitor.done <- trailingMonitorResult{}
			}
			return
		}
		monitor.done <- trailingMonitorResult{}
	}()
	return monitor
}

func (monitor *trailingMonitor) finish(
	ctx context.Context,
	connection net.Conn,
	window time.Duration,
) (bool, bool, bool) {
	if monitor == nil || ctx == nil || connection == nil || window < 0 {
		return false, false, false
	}
	if window != 0 {
		timer := time.NewTimer(window)
		defer timer.Stop()
		select {
		case result := <-monitor.done:
			return result.trailing, result.complete, true
		case <-ctx.Done():
			result, closeVerified, _ := monitor.stopAndWait(ctx, connection)
			return result.trailing, false, closeVerified
		case <-timer.C:
		}
	} else {
		select {
		case result := <-monitor.done:
			return result.trailing, result.complete, true
		default:
		}
	}
	result, closeVerified, joined := monitor.stopAndWait(ctx, connection)
	return result.trailing, result.complete && joined, closeVerified
}

func (monitor *trailingMonitor) stopAndWait(
	ctx context.Context,
	connection net.Conn,
) (trailingMonitorResult, bool, bool) {
	close(monitor.stop)
	closeVerified := true
	if connection.SetReadDeadline(time.Now()) != nil {
		err := connection.Close()
		closeVerified = err == nil || errors.Is(err, net.ErrClosed)
	}
	select {
	case result := <-monitor.done:
		return result, closeVerified, true
	case <-ctx.Done():
		return trailingMonitorResult{}, closeVerified, false
	}
}

func probeConnectionTrailing(
	connection net.Conn,
	remainingBudget int64,
	connectionDeadline time.Time,
) (bool, bool) {
	if connection == nil || remainingBudget <= 0 ||
		connectionDeadline.IsZero() {
		return false, false
	}
	probeDeadline, fullWindow := boundedPhaseDeadline(connectionDeadline, trailingProbeTimeout)
	if connection.SetReadDeadline(probeDeadline) != nil {
		return false, false
	}
	var current [1]byte
	count, err := connection.Read(current[:])
	if count != 0 {
		return true, true
	}
	if errors.Is(err, io.EOF) {
		return false, true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return false, fullWindow
	}
	return false, false
}

func boundedPhaseDeadline(absolute time.Time, window time.Duration) (time.Time, bool) {
	phase := time.Now().Add(window)
	if absolute.Before(phase) {
		return absolute, false
	}
	return phase, true
}

func (origin *scenarioOrigin) closeAdmissions() {
	origin.closeOnce.Do(func() {
		origin.mu.Lock()
		origin.closing = true
		origin.mu.Unlock()
		for _, endpoint := range origin.endpoints {
			if err := endpoint.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				origin.mu.Lock()
				origin.closeFailed = true
				origin.mu.Unlock()
			}
		}
	})
}

func (origin *scenarioOrigin) close(ctx context.Context) bool {
	origin.closeAdmissions()
	origin.mu.Lock()
	connections := make([]net.Conn, 0, len(origin.connections))
	for connection, reading := range origin.connections {
		if reading {
			connections = append(connections, connection)
		}
	}
	origin.mu.Unlock()
	for _, connection := range connections {
		origin.recordConnectionClose(connection.Close())
	}

	serveDone := make(chan struct{})
	go func() {
		for _, endpoint := range origin.endpoints {
			<-endpoint.done
		}
		close(serveDone)
	}()
	handlersDone := make(chan struct{})
	go func() {
		origin.wait.Wait()
		close(handlersDone)
	}()
	if !waitFor(ctx, serveDone) || !waitFor(ctx, handlersDone) {
		origin.mu.Lock()
		remaining := make([]net.Conn, 0, len(origin.connections))
		for connection := range origin.connections {
			remaining = append(remaining, connection)
		}
		origin.captureMissing = true
		origin.closeFailed = true
		origin.mu.Unlock()
		for _, connection := range remaining {
			origin.recordConnectionClose(connection.Close())
		}
		return false
	}
	origin.mu.Lock()
	defer origin.mu.Unlock()
	return !origin.closeFailed && origin.active == 0 && len(origin.connections) == 0
}

func waitFor(ctx context.Context, done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (origin *scenarioOrigin) recordConnectionClose(err error) {
	if err == nil || errors.Is(err, net.ErrClosed) {
		return
	}
	origin.mu.Lock()
	origin.closeFailed = true
	origin.mu.Unlock()
}

func (origin *scenarioOrigin) markCaptureIncomplete() {
	origin.mu.Lock()
	origin.captureMissing = true
	origin.mu.Unlock()
}

func (origin *scenarioOrigin) markCloseFailed() {
	origin.mu.Lock()
	origin.closeFailed = true
	origin.mu.Unlock()
}

func (origin *scenarioOrigin) snapshot() Observation {
	origin.mu.Lock()
	defer origin.mu.Unlock()
	observation := Observation{
		CaptureComplete: !origin.captureMissing && !origin.serveFailed,
		AttemptCount:    uint32(len(origin.attempts)), EffectCount: origin.effectCount,
		OverlapCount: origin.overlapCount, RetryAfterEffectCount: origin.retryAfterEffect,
		RetryAfterUnconfirmedCount: origin.retryAfterUnconfirmed,
		RetryBeforeResponseCount:   origin.retryBeforeResponse,
		ResponseAttemptCount:       origin.responseTry,
		ResponseCompleteCount:      origin.responseDone, FirstResponseComplete: origin.firstResponse,
		DelayCompleteCount: origin.delayDone, MethodConsistent: true,
		DestinationConsistent: true, BodyConsistent: true, Credential: CredentialNotObserved,
		Cleanup: CleanupSucceeded,
	}
	sourceSeen := false
	sourceMissing := false
	targetSeen := false
	targetExposed := false
	for index, attempt := range origin.attempts {
		if attempt.role == redirectEndpoint && attempt.credentialExposed {
			targetExposed = true
		}
		observation.BodyConsistent = observation.BodyConsistent && attempt.bodyConsistent
		if attempt.headersObserved {
			observation.MethodConsistent = observation.MethodConsistent && attempt.methodConsistent
			expectedRole := sourceEndpoint
			sequenceAllowed := true
			if origin.scenario == ScenarioCrossOriginRedirectCredentials && index == 1 {
				expectedRole = redirectEndpoint
			} else if origin.scenario == ScenarioCrossOriginRedirectCredentials && index > 1 {
				sequenceAllowed = false
			}
			observation.DestinationConsistent = observation.DestinationConsistent &&
				attempt.destinationConsistent && attempt.role == expectedRole && sequenceAllowed
		}
		if !attempt.complete {
			continue
		}
		if attempt.role == sourceEndpoint {
			sourceSeen = true
			if !attempt.credentialExact {
				sourceMissing = true
			}
		} else {
			targetSeen = true
		}
	}
	switch {
	case targetExposed:
		observation.Credential = CredentialExposedAtTarget
	case !sourceSeen:
		observation.Credential = CredentialNotObserved
	case sourceMissing:
		observation.Credential = CredentialMissing
	case origin.scenario != ScenarioCrossOriginRedirectCredentials || !targetSeen:
		observation.Credential = CredentialSourceOnly
	default:
		observation.Credential = CredentialAbsentAtTarget
	}
	if origin.closeFailed || origin.active != 0 || len(origin.connections) != 0 {
		observation.Cleanup = CleanupFailed
	}
	return observation
}
