/**
 * Shape of the error object thrown by ofetch/$fetch for failed API requests.
 */
export interface ApiError {
  status?: number
  data?: {
    message?: string
    data?: Record<string, string[]>
  }
}

export function toApiError(error: unknown): ApiError {
  if (error !== null && typeof error === 'object') {
    return error as ApiError
  }
  return {}
}

/** "message_expiration_seconds" -> "Message expiration seconds" */
function humanizeField(field: string): string {
  const words = field.replace(/_/g, ' ').trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

function replaceAll(text: string, search: string, replacement: string): string {
  return text.split(search).join(replacement)
}

/**
 * The per-field validation errors from a 422, as sentences a person can act on.
 *
 * A rejected save answers with a generic top-level message ("validation errors
 * while updating phones") and puts what actually went wrong in `data.data`,
 * keyed by field. Only the generic half was ever shown, so a value the API
 * refused looked like it had saved and silently reverted — with no hint of
 * which field was wrong or what the limit was.
 */
export function getApiErrorDetails(error: unknown): string[] {
  const fields = toApiError(error).data?.data
  if (!fields) {
    return []
  }

  return Object.entries(fields).flatMap(([field, messages]) =>
    (messages ?? []).map((message) => {
      // govalidator's built-in messages embed the raw JSON key, e.g. "The
      // message_expiration_seconds field must be maximum 7200 in size". Swap the
      // key for words in place rather than printing it and a prefix.
      if (message.includes(field)) {
        return replaceAll(message, field, humanizeField(field).toLowerCase())
      }
      // Anything else is a custom message from the validator, written as a bare
      // predicate ("must be at most 7200 (2 hours)") precisely so the field name
      // can be prefixed here. Keep them that way — a message that names its own
      // field will read it twice.
      return `${humanizeField(field)}: ${message}`
    }),
  )
}

export function getApiErrorMessage(error: unknown, fallback: string): string {
  // Field-level detail names the field AND the limit, so it beats the generic
  // top-level message whenever the API sent any.
  const details = getApiErrorDetails(error)
  if (details.length > 0) {
    return details.join(' ')
  }

  return toApiError(error).data?.message ?? fallback
}
