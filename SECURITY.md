# Reporting a security problem

Please report anything you believe is a security problem **privately**, through
GitHub's [Report a vulnerability][advisory] button on the Security tab of this
repository. That opens a draft advisory only you and the maintainers can read.

[advisory]: https://github.com/dennis-dko/go-time-recording/security/advisories/new

Please do not open a public issue for one. An issue is readable by everybody the
moment it is filed, including by anyone who would rather use the problem than
have it fixed — and every installation of this application is somebody's record
of when their colleagues worked.

## What to expect

- An acknowledgement within **three working days**.
- An assessment within **ten working days**: whether it is a problem, how bad,
  and what the fix looks like.
- A fix released as a normal version, with the advisory published once
  installations have had a chance to take it.

You will be credited in the advisory unless you would rather not be.

## What is worth reporting

Anything that lets somebody do what this application says they cannot. The rules
worth knowing before you look, because they are the ones the code is built
around:

- **Nobody may read anybody else's recorded time.** Not an administrator, not
  the built-in account, not through a report, an export, a project total or an
  overtime balance. This is not a permission that happens to be unassigned —
  there is no permission that grants it.
- **The built-in administrator administers and records nothing.** It exists on
  every installation before anybody has chosen anything.
- **Maintenance mode turns everyone away** except accounts that may administer
  the installation.
- **A session ends everywhere when a password changes**, except on the device
  that made the change.

Anything that crosses one of those is worth a report even if you are not sure
how somebody would reach it.

## What is not a vulnerability here

- **Reaching the API with valid credentials.** The API enforces the same rights
  as the interface, per endpoint. That the interface hides what it cannot do is
  a convenience, not the boundary.
- **The initial administrator password.** It is printed once at first start and
  the application refuses to do anything else until it is replaced.
- **Anything requiring the operating-system account** the application runs as.
  Somebody with that has the database file.
- **A missing HTTP header on a deployment behind a proxy that strips it.** Say
  so anyway if the application should be setting it and does not.

## Which versions

The latest release. This is a single self-contained binary with an automatic
update; there are no maintained branches behind it.
