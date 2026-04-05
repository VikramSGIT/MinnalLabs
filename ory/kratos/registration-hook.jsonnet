function(ctx) {
  identity_id: ctx.identity.id,
  username: ctx.identity.traits.username,
  email: std.get(ctx.identity.traits, "email", ""),
}
