# Managed Hosting and Fair Hosting

paperless-gpt is free and open source under the MIT license, and self-hosting is a fully supported way to run it. This page is for people who would rather not operate it themselves.

## Self-hosted or managed

Both are valid ways to run paperless-gpt. They differ in who runs the infrastructure, not in what paperless-gpt can do.

| | Self-hosted | Managed |
|---|---|---|
| Who runs it | You | A hosting provider |
| What you operate | Docker, updates, reverse proxy/authentication, LLM provider or local models | Nothing beyond your account |
| Features | The full open source edition | The same open source edition |
| Cost | Your own hardware and LLM costs | A monthly fee to the provider |
| Where to start | [Getting Started](../README.md#getting-started) | See below |

Self-hosting makes sense if you already run Docker services, want full control over every component, or want to use local models on your own hardware.

Managed hosting makes sense if you want paperless-gpt but don't want to run Docker, keep up with updates, put an authenticating reverse proxy in front of it, or set up AI infrastructure.

## Managed hosting in Germany, Austria and Switzerland

For the DACH region, [server.camp](https://server.camp/product/paperless-ngx) is our Fair Hosting Partner. server.camp offers paperless-ngx as a managed service; paperless-gpt and the AI components it needs are available as an add-on, already set up.

What that means for you:

- paperless-ngx and paperless-gpt ready to use
- hosting and updates managed by server.camp
- no GPU of your own and no separate AI infrastructure to run
- AI processing in the EU

For details on pricing, plans, data location and backups, see [server.camp's paperless-ngx page](https://server.camp/product/paperless-ngx) (in German). Those are server.camp's offering and are described there, not here.

Outside Germany, Austria and Switzerland there is currently no Fair Hosting Partner. Self-hosting works everywhere.

## Fair Hosting

server.camp follows a simple principle: when open source software creates value for their hosting business, the projects behind it should share in that value.

A share of the revenue server.camp generates with paperless-gpt goes back to the paperless-gpt project and funds its continued development. This is not an affiliate or referral arrangement: there is no referral code and no special link. The revenue share applies no matter how a customer found server.camp.

In short: if you need managed hosting anyway, choosing a Fair Hosting Partner also supports the project. If you want to support paperless-gpt directly, [GitHub Sponsors](https://github.com/sponsors/icereed) is the way to do that.

## What stays the same

The partnership is about distribution and funding the project. It does not change how paperless-gpt is built:

- paperless-gpt stays MIT licensed, and the open source edition is not cut down.
- Self-hosting stays fully documented and supported, including Docker Compose and manual setup.
- Hosting partners have no say over the open source roadmap.
- paperless-gpt stays provider-independent. Any hosting provider can run it.
