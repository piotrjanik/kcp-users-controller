/*
Copyright 2025 Piotr Janik.

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

package cognito

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"piotrjanik.dev/users/pkg/userpool"
)

// AWSClient implements the userpool.Client interface for AWS Cognito
type AWSClient struct {
	cognito    *cognitoidentityprovider.Client
	userPoolID string
}

// NewAWSClient creates a new AWS Cognito client with Pod Identity authentication
func NewAWSClient(ctx context.Context, userPoolID string) (*AWSClient, error) {
	if userPoolID == "" {
		return nil, fmt.Errorf("userPoolID cannot be empty")
	}

	// Load AWS configuration with Pod Identity (IRSA)
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &AWSClient{
		cognito:    cognitoidentityprovider.NewFromConfig(cfg),
		userPoolID: userPoolID,
	}, nil
}

// CreateUser creates a new user in the Cognito user pool
func (c *AWSClient) CreateUser(ctx context.Context, user *userpool.User) error {
	if user == nil {
		return fmt.Errorf("user cannot be nil")
	}
	if user.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	attributes := []types.AttributeType{
		{
			Name:  aws.String("email"),
			Value: aws.String(user.Email),
		},
		{
			Name:  aws.String("email_verified"),
			Value: aws.String("true"),
		},
	}

	input := &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId:     aws.String(c.userPoolID),
		Username:       aws.String(user.Username),
		UserAttributes: attributes,
		MessageAction:  types.MessageActionTypeSuppress, // Don't send welcome email
	}

	// User will be enabled by default, we'll handle disabling separately if needed
	if !user.Enabled {
		input.TemporaryPassword = aws.String("TempPass123!")
	}

	_, err := c.cognito.AdminCreateUser(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to create user %s: %w", user.Username, err)
	}

	return nil
}

// GetUser retrieves a user from the Cognito user pool by username
func (c *AWSClient) GetUser(ctx context.Context, username string) (*userpool.User, error) {
	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}

	input := &cognitoidentityprovider.AdminGetUserInput{
		UserPoolId: aws.String(c.userPoolID),
		Username:   aws.String(username),
	}

	output, err := c.cognito.AdminGetUser(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to get user %s: %w", username, err)
	}

	user := &userpool.User{
		Username: username,
		Enabled:  output.Enabled,
	}

	// Extract email from user attributes
	for _, attr := range output.UserAttributes {
		if attr.Name != nil && *attr.Name == "email" && attr.Value != nil {
			user.Email = *attr.Value
			break
		}
	}

	return user, nil
}

// UpdateUser updates an existing user in the Cognito user pool
func (c *AWSClient) UpdateUser(ctx context.Context, user *userpool.User) error {
	if user == nil {
		return fmt.Errorf("user cannot be nil")
	}
	if user.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// Update user attributes
	attributes := []types.AttributeType{
		{
			Name:  aws.String("email"),
			Value: aws.String(user.Email),
		},
	}

	updateInput := &cognitoidentityprovider.AdminUpdateUserAttributesInput{
		UserPoolId:     aws.String(c.userPoolID),
		Username:       aws.String(user.Username),
		UserAttributes: attributes,
	}

	_, err := c.cognito.AdminUpdateUserAttributes(ctx, updateInput)
	if err != nil {
		return fmt.Errorf("failed to update user attributes for %s: %w", user.Username, err)
	}

	// Update user status if needed
	if user.Enabled {
		enableInput := &cognitoidentityprovider.AdminEnableUserInput{
			UserPoolId: aws.String(c.userPoolID),
			Username:   aws.String(user.Username),
		}
		_, err = c.cognito.AdminEnableUser(ctx, enableInput)
		if err != nil {
			return fmt.Errorf("failed to enable user %s: %w", user.Username, err)
		}
	} else {
		disableInput := &cognitoidentityprovider.AdminDisableUserInput{
			UserPoolId: aws.String(c.userPoolID),
			Username:   aws.String(user.Username),
		}
		_, err = c.cognito.AdminDisableUser(ctx, disableInput)
		if err != nil {
			return fmt.Errorf("failed to disable user %s: %w", user.Username, err)
		}
	}

	return nil
}

// DeleteUser removes a user from the Cognito user pool
func (c *AWSClient) DeleteUser(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	input := &cognitoidentityprovider.AdminDeleteUserInput{
		UserPoolId: aws.String(c.userPoolID),
		Username:   aws.String(username),
	}

	_, err := c.cognito.AdminDeleteUser(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete user %s: %w", username, err)
	}

	return nil
}

// ListUsers lists all users in the Cognito user pool
func (c *AWSClient) ListUsers(ctx context.Context) ([]*userpool.User, error) {
	var users []*userpool.User
	var nextToken *string

	for {
		input := &cognitoidentityprovider.ListUsersInput{
			UserPoolId:      aws.String(c.userPoolID),
			PaginationToken: nextToken,
		}

		output, err := c.cognito.ListUsers(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list users: %w", err)
		}

		for _, cognitoUser := range output.Users {
			if cognitoUser.Username == nil {
				continue
			}

			user := &userpool.User{
				Username: *cognitoUser.Username,
				Enabled:  cognitoUser.Enabled,
			}

			// Extract email from user attributes
			for _, attr := range cognitoUser.Attributes {
				if attr.Name != nil && *attr.Name == "email" && attr.Value != nil {
					user.Email = *attr.Value
					break
				}
			}

			users = append(users, user)
		}

		nextToken = output.PaginationToken
		if nextToken == nil {
			break
		}
	}

	return users, nil
}
