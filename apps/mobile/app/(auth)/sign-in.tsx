import { ClerkSignIn } from "@/components/legacy/ClerkSignIn";
import { useLogin } from "@/hooks/mutations/useAuthMutations";
import { isApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errors";
import { BACKEND } from "@/lib/config";
import { SignInFormValues, signInSchema } from "@/lib/schemas/auth";
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

function SignIn() {
  const { mutateAsync: login, isPending: isLoading } = useLogin();
  const [serverError, setServerError] = useState("");

  const {
    control,
    handleSubmit,
    formState: { errors: formErrors },
  } = useForm<SignInFormValues>({
    resolver: zodResolver(signInSchema),
    mode: "onBlur",
    defaultValues: { email: "", password: "" },
  });

  // On success the (auth) layout sees the new session and redirects to "/".
  const onSignInPress = async (values: SignInFormValues) => {
    setServerError("");
    try {
      await login(values);
    } catch (err) {
      setServerError(
        isApiError(err, "invalid_credentials")
          ? "Incorrect email or password."
          : describeError(err)
      );
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
          Welcome back
        </Text>
        <Text className="text-brand-text-muted text-base mb-8">
          Sign in to your account
        </Text>

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
                setServerError("");
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
                setServerError("");
                onChange(v);
              }}
              onBlur={onBlur}
              secureTextEntry
              autoComplete="current-password"
            />
          )}
        />
        {formErrors.password && (
          <Text className="text-brand-coral mb-4 text-sm">
            {formErrors.password.message}
          </Text>
        )}
        {serverError ? (
          <Text className="text-brand-coral mb-4 text-sm">{serverError}</Text>
        ) : null}

        <TouchableOpacity
          onPress={handleSubmit(onSignInPress)}
          disabled={isLoading}
          className="w-full bg-brand-blue py-4 rounded-xl items-center mb-4"
        >
          {isLoading ? (
            <ActivityIndicator color="white" />
          ) : (
            <Text className="text-white font-semibold text-base">Sign In</Text>
          )}
        </TouchableOpacity>

        <View className="flex-row justify-center">
          <Text className="text-brand-text-muted">
            Don&apos;t have an account?{" "}
          </Text>
          <Link href="/sign-up">
            <Text className="text-brand-blue font-semibold">Sign Up</Text>
          </Link>
        </View>
      </View>
    </KeyboardAvoidingView>
  );
}

export default BACKEND === "supabase" ? ClerkSignIn : SignIn;
