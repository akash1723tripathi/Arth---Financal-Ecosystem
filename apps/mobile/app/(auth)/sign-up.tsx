import { ClerkSignUp } from "@/components/legacy/ClerkSignUp";
import { useRegister } from "@/hooks/mutations/useAuthMutations";
import { isApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errors";
import { BACKEND } from "@/lib/config";
import { SignUpFormValues, signUpSchema } from "@/lib/schemas/auth";
import { zodResolver } from "@hookform/resolvers/zod";
import { Link } from "expo-router";
import React, { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import {
  ActivityIndicator,
  Image,
  KeyboardAvoidingView,
  Platform,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";

function SignUpScreen() {
  const { mutateAsync: register, isPending: isLoading } = useRegister();
  const [emailError, setEmailError] = useState("");
  const [passwordError, setPasswordError] = useState("");
  const [serverError, setServerError] = useState("");

  const {
    control,
    handleSubmit,
    formState: { errors: formErrors },
  } = useForm<SignUpFormValues>({
    resolver: zodResolver(signUpSchema),
    mode: "onBlur",
    defaultValues: { firstName: "", lastName: "", email: "", password: "" },
  });

  const clearServerErrors = () => {
    setEmailError("");
    setPasswordError("");
    setServerError("");
  };

  // Email verification needs an email provider (OPEN, D-OPEN-2): the account is usable right away.
  // On success the (auth) layout sees the new session and redirects to "/" (then onboarding).
  const onSignUpPress = async (values: SignUpFormValues) => {
    clearServerErrors();
    try {
      await register({
        email: values.email,
        password: values.password,
        name: `${values.firstName.trim()} ${values.lastName.trim()}`,
      });
    } catch (err) {
      if (isApiError(err, "email_taken")) {
        setEmailError("An account with this email already exists.");
      } else if (isApiError(err, "weak_password")) {
        setPasswordError(
          "Choose a stronger password: 8–128 characters and not a common password."
        );
      } else if (isApiError(err, "validation_failed")) {
        setServerError("Please check your details and try again.");
      } else {
        setServerError(describeError(err));
      }
    }
  };

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : "height"}
      className="flex-1 bg-brand-body"
    >
      <View className="flex-1 justify-center px-6 -mt-16">
        <Image
          source={require("../../assets/images/welth.png")}
          className="w-36 h-16 mb-8"
          resizeMode="contain"
        />
        <Text className="text-3xl font-bold text-[#1A1D26] mb-2 leading-tight">
          Create account
        </Text>
        <Text className="text-brand-text-muted text-base mb-8">
          Track your money, powered by AI
        </Text>

        <View className="flex-row gap-3 mb-2">
          <Controller
            control={control}
            name="firstName"
            render={({ field: { value, onChange, onBlur } }) => (
              <TextInput
                className="flex-1 border border-[#E8E6DF] bg-white rounded-xl px-4 py-3 text-[#1A1D26]"
                placeholder="First name"
                placeholderTextColor="#8A8D96"
                value={value}
                onChangeText={onChange}
                onBlur={onBlur}
                autoCapitalize="words"
              />
            )}
          />
          <Controller
            control={control}
            name="lastName"
            render={({ field: { value, onChange, onBlur } }) => (
              <TextInput
                className="flex-1 border border-[#E8E6DF] bg-white rounded-xl px-4 py-3 text-[#1A1D26]"
                placeholder="Last name"
                placeholderTextColor="#8A8D96"
                value={value}
                onChangeText={onChange}
                onBlur={onBlur}
                autoCapitalize="words"
              />
            )}
          />
        </View>
        {(formErrors.firstName || formErrors.lastName) && (
          <Text className="text-brand-coral mb-4 text-sm">
            {formErrors.firstName?.message || formErrors.lastName?.message}
          </Text>
        )}

        <Controller
          control={control}
          name="email"
          render={({ field: { value, onChange, onBlur } }) => (
            <TextInput
              className="border border-[#E8E6DF] bg-white rounded-xl px-4 py-3 mb-2 text-[#1A1D26]"
              placeholder="Email Address"
              placeholderTextColor="#8A8D96"
              value={value}
              onChangeText={(v) => {
                clearServerErrors();
                onChange(v);
              }}
              onBlur={onBlur}
              autoCapitalize="none"
              autoComplete="email"
              keyboardType="email-address"
            />
          )}
        />
        {formErrors.email && (
          <Text className="text-brand-coral mb-4 text-sm">
            {formErrors.email.message}
          </Text>
        )}
        {emailError ? (
          <Text className="text-brand-coral mb-4 text-sm">{emailError}</Text>
        ) : null}

        <Controller
          control={control}
          name="password"
          render={({ field: { value, onChange, onBlur } }) => (
            <TextInput
              className="border border-[#E8E6DF] bg-white rounded-xl px-4 py-3 mb-2 text-[#1A1D26]"
              placeholder="Password"
              placeholderTextColor="#8A8D96"
              value={value}
              onChangeText={(v) => {
                clearServerErrors();
                onChange(v);
              }}
              onBlur={onBlur}
              secureTextEntry
              autoComplete="new-password"
            />
          )}
        />
        {formErrors.password && (
          <Text className="text-brand-coral mb-4 text-sm">
            {formErrors.password.message}
          </Text>
        )}
        {passwordError ? (
          <Text className="text-brand-coral mb-4 text-sm">{passwordError}</Text>
        ) : null}
        {serverError ? (
          <Text className="text-brand-coral mb-4 text-sm">{serverError}</Text>
        ) : null}

        <TouchableOpacity
          onPress={handleSubmit(onSignUpPress)}
          disabled={isLoading}
          className="w-full bg-brand-blue py-4 rounded-xl items-center mb-4"
        >
          {isLoading ? (
            <ActivityIndicator color="white" />
          ) : (
            <Text className="text-white font-semibold text-base">Sign Up</Text>
          )}
        </TouchableOpacity>

        <View className="flex-row justify-center">
          <Text className="text-brand-text-muted">
            Already have an account?{" "}
          </Text>
          <Link href="/sign-in">
            <Text className="text-brand-blue font-semibold">Sign In</Text>
          </Link>
        </View>
      </View>
    </KeyboardAvoidingView>
  );
}

export default BACKEND === "supabase" ? ClerkSignUp : SignUpScreen;
