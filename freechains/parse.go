package freechains

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseOrder parses the output of "chain <alias> list order".
func ParseOrder(r io.Reader) ([]OrderEntry, error) {
	scanner := bufio.NewScanner(r)
	var entries []OrderEntry

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		revoked := false
		if strings.HasPrefix(line, "~") && strings.HasSuffix(line, "~") && len(line) >= 2 {
			revoked = true
			line = line[1 : len(line)-1]
		}

		id := ActionID(line)
		if err := id.Validate(); err != nil {
			return nil, fmt.Errorf("%w: invalid action id in order: %q", err, line)
		}

		entries = append(entries, OrderEntry{
			ID:      id,
			Revoked: revoked,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// ParseActionIDs parses lines containing action IDs (e.g., list begs, list revokes).
func ParseActionIDs(r io.Reader) ([]ActionID, error) {
	scanner := bufio.NewScanner(r)
	var ids []ActionID

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		id := ActionID(line)
		if err := id.Validate(); err != nil {
			return nil, fmt.Errorf("%w: invalid action id: %q", err, line)
		}
		ids = append(ids, id)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// ParseChainsDir parses the output of "chains dir", expecting one alias per line.
func ParseChainsDir(r io.Reader) ([]Alias, error) {
	scanner := bufio.NewScanner(r)
	var aliases []Alias

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		alias := Alias(line)
		if err := alias.Validate(); err != nil {
			return nil, fmt.Errorf("%w: invalid chain alias: %q", err, line)
		}
		aliases = append(aliases, alias)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return aliases, nil
}

// ParseMetadata parses the key-value output of "chain <alias> get metadata <id>".
func ParseMetadata(r io.Reader) (Metadata, error) {
	scanner := bufio.NewScanner(r)
	var m Metadata
	isFirstLine := true

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if isFirstLine {
			id := ActionID(line)
			if err := id.Validate(); err != nil {
				return Metadata{}, fmt.Errorf("%w: invalid metadata action id: %q", err, line)
			}
			m.ID = id
			isFirstLine = false
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		key := parts[0]
		val := ""
		if len(parts) > 1 {
			val = strings.TrimSpace(parts[1])
		}

		switch key {
		case "action":
			switch val {
			case "post":
				m.Kind = ActionPost
			case "like":
				m.Kind = ActionLike
			case "revoke":
				m.Kind = ActionRevoke
			default:
				m.Kind = ActionKind(val)
			}
		case "time":
			t, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return Metadata{}, fmt.Errorf("%w: invalid metadata time: %q", ErrInvalidArgument, val)
			}
			m.Time = t
		case "n":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return Metadata{}, fmt.Errorf("%w: invalid metadata n: %q", ErrInvalidArgument, val)
			}
			m.Amount = &n
		case "cid":
			actID := ActionID(val)
			if err := actID.Validate(); err != nil {
				return Metadata{}, fmt.Errorf("%w: invalid metadata cid: %q", err, val)
			}
			m.TargetAction = &actID
		case "author":
			pub := PublicKey(val)
			if err := pub.Validate(); err != nil {
				return Metadata{}, fmt.Errorf("%w: invalid metadata author: %q", err, val)
			}
			m.TargetAuthor = &pub
		case "blob":
			m.Blob = val
		case "sign":
			pub := PublicKey(val)
			if err := pub.Validate(); err != nil {
				return Metadata{}, fmt.Errorf("%w: invalid metadata sign: %q", err, val)
			}
			m.Signer = &pub
		case "backs":
			if val != "" {
				rawBacks := strings.Fields(val)
				for _, b := range rawBacks {
					backID := ActionID(b)
					if err := backID.Validate(); err != nil {
						return Metadata{}, fmt.Errorf("%w: invalid metadata back id: %q", err, b)
					}
					m.Backs = append(m.Backs, backID)
				}
			}
		default:
			// Unknown keys are ignored for forward compatibility.
		}
	}

	if err := scanner.Err(); err != nil {
		return Metadata{}, err
	}

	if isFirstLine {
		return Metadata{}, fmt.Errorf("%w: metadata output was empty", ErrInvalidArgument)
	}

	return m, nil
}

// ParseInt64 parses a single integer from reader output.
func ParseInt64(r io.Reader) (int64, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: expected integer: %q", ErrInvalidArgument, line)
		}
		return n, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("%w: empty output when integer expected", ErrInvalidArgument)
}

// ParseChainID parses the chain ID from chains add stdout (last non-empty line).
func ParseChainID(r io.Reader) (ChainID, error) {
	scanner := bufio.NewScanner(r)
	var lastLine string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lastLine = line
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastLine == "" {
		return "", fmt.Errorf("%w: empty output when chain id expected", ErrInvalidArgument)
	}
	cid := ChainID(lastLine)
	if err := cid.Validate(); err != nil {
		return "", err
	}
	return cid, nil
}

// ParseActionID parses the created action ID from command output (last non-empty line).
func ParseActionID(r io.Reader) (ActionID, error) {
	scanner := bufio.NewScanner(r)
	var lastLine string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lastLine = line
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastLine == "" {
		return "", fmt.Errorf("%w: empty output when action id expected", ErrInvalidArgument)
	}
	id := ActionID(lastLine)
	if err := id.Validate(); err != nil {
		return "", err
	}
	return id, nil
}

// ParseRepsActions parses lines of "<action_id> <reps>".
func ParseRepsActions(r io.Reader) ([]ActionRep, error) {
	scanner := bufio.NewScanner(r)
	var reps []ActionRep

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 2 {
			return nil, fmt.Errorf("%w: invalid reps actions line format: %q", ErrInvalidArgument, line)
		}

		id := ActionID(parts[0])
		if err := id.Validate(); err != nil {
			return nil, err
		}

		val, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid reps value: %q", ErrInvalidArgument, parts[1])
		}

		reps = append(reps, ActionRep{
			ID:   id,
			Reps: val,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return reps, nil
}

// ParseRepsAuthors parses lines of "<pub> <reps>".
func ParseRepsAuthors(r io.Reader) ([]AuthorRep, error) {
	scanner := bufio.NewScanner(r)
	var reps []AuthorRep

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		lastSpace := strings.LastIndex(line, " ")
		if lastSpace < 0 {
			return nil, fmt.Errorf("%w: invalid reps authors line format: %q", ErrInvalidArgument, line)
		}

		pubStr := strings.TrimSpace(line[:lastSpace])
		repsStr := strings.TrimSpace(line[lastSpace+1:])

		pub := PublicKey(pubStr)
		if err := pub.Validate(); err != nil {
			return nil, err
		}

		val, err := strconv.ParseInt(repsStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid reps value: %q", ErrInvalidArgument, repsStr)
		}

		reps = append(reps, AuthorRep{
			Author: pub,
			Reps:   val,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return reps, nil
}

// ParseRepsRevokes parses lines of "<action_id> <author_reps> <others_reps>".
func ParseRepsRevokes(r io.Reader) ([]RevokeRep, error) {
	scanner := bufio.NewScanner(r)
	var reps []RevokeRep

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 3 {
			return nil, fmt.Errorf("%w: invalid reps revokes line format: %q", ErrInvalidArgument, line)
		}

		id := ActionID(parts[0])
		if err := id.Validate(); err != nil {
			return nil, err
		}

		author, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid author reps value: %q", ErrInvalidArgument, parts[1])
		}

		others, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid others reps value: %q", ErrInvalidArgument, parts[2])
		}

		reps = append(reps, RevokeRep{
			Target: id,
			Author: author,
			Others: others,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return reps, nil
}

// ParseRepsRevoke parses "<author_reps> <others_reps>" from reps revoke <id>.
func ParseRepsRevoke(r io.Reader) (RevokeReps, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 2 {
			return RevokeReps{}, fmt.Errorf("%w: expected two integers for reps revoke: %q", ErrInvalidArgument, line)
		}

		author, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return RevokeReps{}, fmt.Errorf("%w: invalid author reps value: %q", ErrInvalidArgument, parts[0])
		}

		others, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return RevokeReps{}, fmt.Errorf("%w: invalid others reps value: %q", ErrInvalidArgument, parts[1])
		}

		return RevokeReps{
			Author: author,
			Others: others,
		}, nil
	}

	if err := scanner.Err(); err != nil {
		return RevokeReps{}, err
	}
	return RevokeReps{}, fmt.Errorf("%w: empty output when reps revoke expected", ErrInvalidArgument)
}
