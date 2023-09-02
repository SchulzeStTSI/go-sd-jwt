// Package go_sd_jwt provides a library for creating and validating SD-JWTs.
// The resulting SdJwt object exposes methods for retrieving the claims and
// disclosures as well as retrieving all disclosed claims in line with the specification.
package go_sd_jwt

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash"
	"reflect"
	"strings"
)

// SdJwt this object represents a valid SD-JWT. Created using the New function which performs the required validation.
// Helper methods are provided for retrieving the contents
type SdJwt struct {
	token       string
	head        map[string]any
	body        map[string]any
	signature   string
	disclosures []Disclosure
}

// Disclosure this object represents a single disclosure in a SD-JWT.
// Helper methods are provided for retrieving the contents
type Disclosure struct {
	salt         string
	claimName    *string
	claimValue   string
	rawValue     string
	encodedValue string
}

type jwsSdJwt struct {
	Payload     *string  `json:"payload"`
	Protected   *string  `json:"protected"`
	Signature   *string  `json:"signature"`
	Disclosures []string `json:"disclosures"`
	KbJwt       *string  `json:"kb_jwt"`
}

type arrayDisclosure struct {
	Digest *string `json:"..."`
}

// New
// Creates a new SD-JWT from a JWS or JWT format token.
// The token is validated inline with the SD-JWT specification.
// If the token is valid, a new SdJwt object is returned.
func New(token string) (*SdJwt, error) {
	jwsSdjwt := jwsSdJwt{}
	err := json.Unmarshal([]byte(token), &jwsSdjwt)
	if err == nil {
		if jwsSdjwt.Payload != nil && jwsSdjwt.Protected != nil && jwsSdjwt.Signature != nil {
			return validateJws(jwsSdjwt)
		} else {
			return nil, errors.New("invalid JWS format SD-JWT provided")
		}
	} else {
		return validateJwt(token)
	}
	//todo: validate jwt
	//todo: reject if duplicate digests found
}

func validateJws(token jwsSdJwt) (*SdJwt, error) {
	sdJwt := &SdJwt{}

	b, err := json.Marshal(token)
	if err != nil {
		return nil, err
	}
	sdJwt.token = string(b)

	hb, err := base64.RawURLEncoding.DecodeString(*token.Protected)
	if err != nil {
		return nil, err
	}
	var head map[string]any
	err = json.Unmarshal(hb, &head)
	if err != nil {
		return nil, err
	}
	sdJwt.head = head

	sdJwt.signature = *token.Signature

	disclosures, err := validateDisclosures(token.Disclosures)
	if err != nil {
		return nil, err
	}

	sdJwt.disclosures = disclosures

	b, err = base64.RawURLEncoding.DecodeString(*token.Payload)
	if err != nil {
		return nil, err
	}

	var m map[string]any
	err = json.Unmarshal(b, &m)
	if err != nil {
		return nil, err
	}

	err = validateDigests(m)
	if err != nil {
		return nil, err
	}

	sdJwt.body = m

	return sdJwt, nil
}

func validateJwt(token string) (*SdJwt, error) {
	sdJwt := &SdJwt{}

	sections := strings.Split(token, "~")
	if len(sections) < 2 {
		return nil, errors.New("token has no specified disclosures")
	}

	sdJwt.token = sections[0]

	tokenSections := strings.Split(token, ".")

	if len(tokenSections) != 3 {
		return nil, errors.New("token is not a valid JWT")
	}

	jwtHead := map[string]any{}
	hb, err := base64.RawURLEncoding.DecodeString(tokenSections[0])
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(hb, &jwtHead)
	if err != nil {
		return nil, err
	}

	sdJwt.head = jwtHead

	sdJwt.signature = tokenSections[2]

	disclosures, err := validateDisclosures(sections[1:])
	if err != nil {
		return nil, err
	}
	sdJwt.disclosures = disclosures

	b, err := base64.RawURLEncoding.DecodeString(tokenSections[1])
	if err != nil {
		return nil, err
	}

	var m map[string]any
	err = json.Unmarshal(b, &m)
	if err != nil {
		return nil, err
	}

	err = validateDigests(m)
	if err != nil {
		return nil, err
	}

	digests := getDigests(m)

	for _, d := range digests {
		count := 0
		for _, d2 := range sdJwt.disclosures {
			if d == d2 {
				count++
			}
		}
		if count > 1 {
			return nil, errors.New("duplicate digest found")
		}
	}

	sdJwt.body = m

	return sdJwt, nil
}

func newDisclosure(d []byte) (*Disclosure, error) {
	decodedDisclosure, err := base64.RawURLEncoding.DecodeString(string(d))
	if err != nil {
		return nil, err
	}
	if decodedDisclosure[0] != '[' || decodedDisclosure[len(decodedDisclosure)-1] != ']' {
		return nil, errors.New("provided decoded disclosure is not a valid array")
	}

	disclosure := &Disclosure{}

	parts := strings.Split(string(decodedDisclosure[1:len(decodedDisclosure)-1]), ",")

	disclosure.setRawValue(string(decodedDisclosure))
	disclosure.setEncodedValue(string(d))
	if len(parts) == 2 {
		disclosure.setSalt(*cleanStr(parts[0]))
		disclosure.setClaimValue(*cleanStr(parts[1]))
	} else {
		parts[2] = strings.Join(parts[2:], ",")
		parts = parts[:3]

		if len(parts) != 3 {
			return nil, errors.New("provided decoded disclosure does not have all required parts")
		}

		disclosure.setSalt(*cleanStr(parts[0]))
		disclosure.setClaimName(cleanStr(parts[1]))
		disclosure.setClaimValue(*cleanStr(parts[2]))
	}
	return disclosure, nil
}

func cleanStr(s string) *string {
	return Pointer(strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "\"")))
}

func validateDisclosures(disclosures []string) ([]Disclosure, error) {
	var disclosureArray []Disclosure

	if len(disclosures) == 0 {
		return nil, errors.New("token has no specified disclosures")
	}

	for _, d := range disclosures {
		count := 0
		if d != "" {
			for _, d2 := range disclosures {
				if d == d2 {
					count++
				}
			}
			if count > 1 {
				return nil, errors.New("duplicate disclosure found")
			}
			dis, err := newDisclosure([]byte(d))
			if err != nil {
				return nil, err
			}
			disclosureArray = append(disclosureArray, *dis)
		}
	}
	return disclosureArray, nil
}

func validateDigests(body map[string]interface{}) error {
	digests := getDigests(body)

	for _, d := range digests {
		count := 0
		for _, d2 := range digests {
			if d == d2 {
				count++
			}
		}
		if count > 1 {
			return errors.New("duplicate digest found")
		}
	}
	return nil
}

// GetDisclosedClaims returns the claims that were disclosed in the token or included as plaintext values.
// This function will error one of the following scenarios is encountered:
// 1. The SD-JWT contains a disclosure that does not match an included digest
// 2. The SD-JWT contains a malformed _sd claim
// 3. The SD-JWT contains an unsupported value for the _sd_alg claim
// 4. The SD-JWT has a disclosure that is malformed for the use (e.g. doesn't contain a claim name for a non-array digest)
func (s *SdJwt) GetDisclosedClaims() (map[string]any, error) {
	bodyMap := make(map[string]any)

	disclosuresToCheck := make([]Disclosure, len(s.disclosures))
	copy(disclosuresToCheck, s.disclosures)
	for len(disclosuresToCheck) > 0 {
		d := disclosuresToCheck[0]

		var h hash.Hash

		switch s.body["_sd_alg"] {
		case "none":
			return nil, errors.New("none is not a valid algorithm")
		case "sha-256":
			h = sha256.New()
		}

		h.Write([]byte(d.EncodedValue()))
		hashedDisclosures := h.Sum(nil)
		base64HashedDisclosureBytes := make([]byte, base64.RawURLEncoding.EncodedLen(len(hashedDisclosures)))
		base64.RawURLEncoding.Encode(base64HashedDisclosureBytes, hashedDisclosures)

		found, err := validateSDClaims(s.Body(), &d, string(base64HashedDisclosureBytes))
		if err != nil {
			return nil, err
		}

		if !found {
			return nil, errors.New("no matching digest found: " + d.RawValue() + " encoded: " + string(base64HashedDisclosureBytes))
		}

		if len(disclosuresToCheck) > 1 {
			disclosuresToCheck = disclosuresToCheck[1:]
		} else {
			disclosuresToCheck = []Disclosure{} //empty to-check array
		}

	}

	for k, v := range s.body {
		if k != "_sd" && k != "_sd_alg" {
			bodyMap[k] = v
		}
	}

	return bodyMap, nil
}

func getDigests(m map[string]any) []any {
	var digests []any
	for k, v := range m {
		if reflect.TypeOf(v).Kind() == reflect.Map {
			digests = append(digests, getDigests(v.(map[string]any))...)
		} else if k == "_sd" {
			digests = append(digests, v.([]any)...)
		} else if reflect.TypeOf(v).Kind() == reflect.Slice {
			for _, v2 := range v.([]any) {
				b, err := json.Marshal(v2)
				if err == nil {
					var arrayDisclosure arrayDisclosure
					err = json.Unmarshal(b, &arrayDisclosure)
					if err == nil {
						digests = append(digests, *arrayDisclosure.Digest)
					}
				}
			}
		}
	}
	return digests
}

func parseClaimValue(cv string) (any, error) {
	var m map[string]any
	var s []any
	var b bool
	var i int

	err := json.Unmarshal([]byte(cv), &m)
	if err == nil {
		return m, nil
	}

	err = json.Unmarshal([]byte(cv), &s)
	if err == nil {
		return s, nil
	}

	err = json.Unmarshal([]byte(cv), &b)
	if err == nil {
		return b, nil
	}

	err = json.Unmarshal([]byte(cv), &i)
	if err == nil {
		return i, nil
	}

	//Return string as a fallback
	return cv, nil
}

func validateSDClaims(values *map[string]any, currentDisclosure *Disclosure, base64HashedDisclosure string) (found bool, err error) {
	if _, ok := (*values)["_sd"]; ok {
		for _, digest := range (*values)["_sd"].([]any) {
			if digest == base64HashedDisclosure {
				if currentDisclosure.ClaimName() != nil {
					val, err := parseClaimValue(currentDisclosure.ClaimValue())
					if err != nil {
						return false, err
					}
					(*values)[*currentDisclosure.ClaimName()] = val
					return true, nil
				} else {
					return false, errors.New("invalid disclosure format for _sd claim")
				}
			}
		}
	}

	for k, v := range *values {
		if k != "_sd" && k != "_sd_alg" {
			if reflect.TypeOf(v).Kind() == reflect.Slice {
				found, err = validateArrayClaims(PointerSlice(v.([]any)), currentDisclosure, base64HashedDisclosure)
				if err != nil {
					return false, err
				}
			} else if reflect.TypeOf(v).Kind() == reflect.Map {
				found, err = validateSDClaims(PointerMap(v.(map[string]any)), currentDisclosure, base64HashedDisclosure)
				if err != nil {
					return found, err
				}
			}
			if found {
				return true, nil
			}
		}
	}
	return false, nil
}

func validateArrayClaims(s *[]any, currentDisclosure *Disclosure, base64HashedDisclosure string) (found bool, err error) {

	for i, v := range *s {
		ad := &arrayDisclosure{}
		vb, err := json.Marshal(v)
		if err != nil {
			return false, err
		}

		_ = json.Unmarshal(vb, ad)

		if ad.Digest != nil {
			if *ad.Digest == base64HashedDisclosure {
				(*s)[i] = currentDisclosure.ClaimValue()
				return true, nil
			}
		}

		if reflect.TypeOf(v).Kind() == reflect.Slice {
			found, err = validateArrayClaims(PointerSlice(v.([]any)), currentDisclosure, base64HashedDisclosure)
			if err != nil {
				return found, err
			}
		}

		if reflect.TypeOf(v).Kind() == reflect.Map {
			found, err = validateSDClaims(PointerMap(v.(map[string]any)), currentDisclosure, base64HashedDisclosure)
			if err != nil {
				return found, err
			}
		}
	}

	return false, nil
}

// Body returns the body of the JWT
func (s *SdJwt) Body() *map[string]any {
	return &s.body
}

// Token returns the JWT token as it was received
func (s *SdJwt) Token() string {
	return s.token
}

// Signature returns the signature of the provided token used to verify it
func (s *SdJwt) Signature() string {
	return s.signature
}

// Head returns the head of the JWT
func (s *SdJwt) Head() map[string]any {
	return s.head
}

// Disclosures returns the disclosures of the SD-JWT
func (s *SdJwt) Disclosures() []Disclosure {
	return s.disclosures
}

// ClaimName returns the claim name of the disclosure
func (d *Disclosure) ClaimName() *string {
	return d.claimName
}

// ClaimValue returns the claim value of the disclosure
func (d *Disclosure) ClaimValue() string {
	return d.claimValue
}

// Salt returns the salt of the disclosure
func (d *Disclosure) Salt() string {
	return d.salt
}

// RawValue returns the decoded contents of the disclosure
func (d *Disclosure) RawValue() string {
	return d.rawValue
}

// EncodedValue returns the disclosure as it was listed in the original SD-JWT
func (d *Disclosure) EncodedValue() string {
	return d.encodedValue
}

func (d *Disclosure) setClaimName(claimName *string) {
	d.claimName = claimName
}

func (d *Disclosure) setClaimValue(claimValue string) {
	d.claimValue = claimValue
}

func (d *Disclosure) setSalt(salt string) {
	d.salt = salt
}

func (d *Disclosure) setRawValue(rawValue string) {
	d.rawValue = rawValue
}

func (d *Disclosure) setEncodedValue(encodedValue string) {
	d.encodedValue = encodedValue
}

// Pointer is a helper method that returns a pointer to the given value.
func Pointer[T comparable](t T) *T {
	return &t
}

// PointerMap is a helper method that returns a pointer to the given map.
func PointerMap(m map[string]any) *map[string]any {
	return &m
}

// PointerSlice is a helper method that returns a pointer to the given slice.
func PointerSlice(s []any) *[]any {
	return &s
}
