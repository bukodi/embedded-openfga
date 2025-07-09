# About

This repo demonstrates how to embed OpenFGA in your Go backend application and perform the following authz operations:

- PDP (Policy Decision Point) - to check if a user has access to a resource.
- PEP (Policy Enforcement Point) - to enforce the access control policies, done in the Gin handler.
- PAP (Policy Administration Point) - to manage the policies, e.g. add a new tuple for a user to access a resource.


## Getting started

```bash
make run # or docker compose up --build
```

Then open your browser and go to `http://localhost:8007`. The login page will offer you the ability to log with one of two users.

You can adjust initial access via the `INITIAL_TUPLES` environment variable in the `compose.yml` file.