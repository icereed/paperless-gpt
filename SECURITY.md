# Security Policy

## Reporting a Vulnerability

At paperless-gpt, we take security seriously. If you discover a security vulnerability, please follow these steps:

1. **DO NOT** disclose the vulnerability publicly.
2. Send a detailed report to security@icereed.net including:
   - A description of the vulnerability
   - Steps to reproduce the issue
   - Potential impact
   - Any suggested fixes (if available)
3. Allow up to 48 hours for an initial response.
4. Please do not disclose the issue publicly until we've had a chance to address it.

## Security Considerations

### API Keys and Tokens
- Never commit API keys, tokens, or sensitive credentials to the repository
- Use environment variables for all sensitive configuration
- Rotate API keys and tokens regularly
- Use the minimum required permissions for API tokens

### Data Privacy
- All document processing is done locally or via your configured LLM provider
- No document data is stored permanently outside your system
- Temporary files are cleaned up after processing
- Documents are transmitted securely using HTTPS

### Docker Security
- Containers run with minimal privileges
- Images are regularly updated with security patches
- Dependencies are scanned for vulnerabilities
- Official base images are used

### LLM Provider Security
- API calls to LLM providers use encrypted connections
- Rate limiting is implemented to prevent abuse
- Input validation is performed on all user inputs
- Error messages are sanitized to prevent information leakage

### Access Control
- Use strong passwords for all services
- Implement the principle of least privilege
- Regular security audits of access controls
- Monitor for unauthorized access attempts

## Version Support

We provide security updates for:
- The current major version
- The previous major version for 6 months after a new major release

## Best Practices for Deployment

1. **Network Security**
   - Use HTTPS for all connections
   - Implement proper firewall rules
   - Use secure DNS configurations
   - Regular security audits

2. **System Updates**
   - Keep all system packages updated
   - Subscribe to security advisories
   - Regular vulnerability scanning
   - Automated update notifications

3. **Monitoring**
   - Monitor system logs for suspicious activity
   - Track resource usage patterns
   - Alert on anomalous behavior
   - Regular security assessments

4. **Backup and Recovery**
   - Regular backups of critical data
   - Secure backup storage
   - Tested recovery procedures
   - Documented incident response plan

## Dependencies

We regularly monitor and update dependencies for security vulnerabilities:
- Automated dependency updates via Renovate
- Regular security audits of dependencies
- Minimal use of third-party packages
- Verification of package signatures

## Contributing Security Fixes

If you want to contribute security fixes:
1. Follow the standard pull request process
2. Mark security-related PRs as "security fix"
3. Provide detailed description of the security impact
4. Include tests that verify the fix

## Security Release Process

When a security issue is identified:
1. Issue is assessed and prioritized
2. Fix is developed and tested
3. Security advisory is prepared
4. Fix is deployed and announced
5. Users are notified through appropriate channels

## Incident Response

In case of a security incident:
1. Issue is immediately assessed
2. Affected systems are isolated
3. Root cause is identified
4. Fix is developed and tested
5. Systems are restored
6. Incident report is prepared
7. Preventive measures are implemented

## Contact

For security-related matters, contact:
- Email: security@icereed.net
- Response time: Within 48 hours
- Language: English

## Acknowledgments

We'd like to thank all security researchers who have helped improve the security of paperless-gpt. A list of acknowledged researchers can be found in our [Hall of Fame](CONTRIBUTORS.md#security-researchers).
