# apt-s3

## under construction.

this repository is testing.

```
                      ┌──────────┐
                      │          │    for lock, private key store.
                      │ config   │◄───────────────────┐
                      │ Bucket   │                    │
                      └──────────┘                    │
                                                      │
┌──────────────┐      ┌──────────┐            ┌───────┴──────────┐
│              │      │          │  event     │                  │
│ you          ├─────►│ incoming ├───────────►│  lambda          │
│ put a .deb   │      │ Bucket   │            │                  │
└──────────────┘      └────┬─────┘            └────┬─────────────┘
                           │ copy deb file         │
                           │      by lambda.       │
                           ▼                       │
                    ┌────────────┐                 │
                    │            │ ◄───────────────┘
      ────────────► │ repository │   generate Packages, Release and more.
read a              │ Bucket     │
static website      └────────────┘
```

## feature

- store and serve deb file.
- generate InRelease, and more.
- simple lock via s3
- regenerate InRelease via no inputs invoke.

