export function formatTimestamp(value, fallback = "Not seen yet") {
  if (!value) {
    return fallback;
  }

  const timestamp = new Date(value);
  if (Number.isNaN(timestamp.getTime())) {
    return fallback;
  }

  return timestamp.toLocaleString();
}
