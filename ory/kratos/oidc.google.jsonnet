local claims = std.extVar('claims');

{
  identity: {
    traits: {
      username: if std.length(std.get(claims, "email", "")) > 0 then
        std.split(claims.email, "@")[0]
      else
        claims.sub,
      [if std.length(std.get(claims, "email", "")) > 0 then "email"]: claims.email,
    },
  },
}
