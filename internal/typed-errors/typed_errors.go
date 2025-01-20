package typederrors

import "errors"

// GenericError is an error structure containing common fields to be
// embedded by specific error types defined below
type GenericError struct {
	Message string
	Err     error
}

func (ge GenericError) Error() string {
	return ge.Message
}

func (ge GenericError) Unwrap() error {
	return ge.Err
}

// ConfigMapError type

type ConfigMapError struct {
	GenericError
}

func NewConfigMapError(m string, e error) error {
	return ConfigMapError{
		GenericError: GenericError{m, e},
	}
}

func IsConfigMapError(target error) bool {
	var e ConfigMapError
	return errors.As(target, &e)
}

// TokenError type

type TokenError struct {
	GenericError
}

func NewTokenError(m string, e error) error {
	return TokenError{
		GenericError: GenericError{m, e},
	}
}

func IsTokenError(target error) bool {
	var e TokenError
	return errors.As(target, &e)
}

// SecretError type

type SecretError struct {
	GenericError
}

func NewSecretError(m string, e error) error {
	return TokenError{
		GenericError: GenericError{m, e},
	}
}

func IsSecretError(target error) bool {
	var e SecretError
	return errors.As(target, &e)
}
