/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	return SecretError{
		GenericError: GenericError{m, e},
	}
}

func IsSecretError(target error) bool {
	var e SecretError
	return errors.As(target, &e)
}

// RetriableError type
type RetriableError struct {
	GenericError
}

func NewRetriableError(m string, e error) error {
	return RetriableError{
		GenericError: GenericError{m, e},
	}
}

func IsRetriableError(target error) bool {
	var e RetriableError
	return errors.As(target, &e)
}

// NonRetriableError type
type NonRetriableError struct {
	GenericError
}

func NewNonRetriableError(m string, e error) error {
	return NonRetriableError{
		GenericError: GenericError{m, e},
	}
}

func IsNonRetriableError(target error) bool {
	var e NonRetriableError
	return errors.As(target, &e)
}
